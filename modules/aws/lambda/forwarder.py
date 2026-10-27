"""Sends logs that AWS services write to S3 to Tailcove.

Many AWS services cannot stream: load balancers, CloudFront, S3 access logs, VPC flow logs, CloudTrail and WAF write files to a
bucket. This function is triggered by the bucket's object-created notification, reads the new object, turns each line into a small JSON
document and posts them to Tailcove's Amazon Data Firehose endpoint (the one a delivery stream posts to), so the parsing of what is in
the documents lives in one place, on the Tailcove side.

Standard library only (boto3 is part of the Lambda runtime). Configuration, from the environment:

    TAILCOVE_URL    the ingest base URL, e.g. https://ingest.tailcove.dev
    TAILCOVE_TOKEN  the source's ingest token
"""

import base64
import gzip
import hashlib
import io
import json
import os
import re
import time
import urllib.error
import urllib.request
from urllib.parse import unquote_plus

MAX_BATCH_RECORDS = 500
MAX_BATCH_BYTES = 2 * 1024 * 1024
MAX_LINE_BYTES = 64 * 1024

_QUOTED = re.compile(r'"([^"]*)"|\[([^\]]*)\]|(\S+)')


def split_fields(line):
    """Splits a space-separated log line whose fields may be "quoted" or [bracketed] (ALB, S3 access logs)."""
    fields = []
    for match in _QUOTED.finditer(line):
        fields.append(match.group(1) if match.group(1) is not None else match.group(2) if match.group(2) is not None else match.group(3))
    return fields


def _level(status):
    try:
        status = int(status)
    except (TypeError, ValueError):
        return "info"
    return "error" if status >= 500 else "warning" if status >= 400 else "info"


# --- the formats ------------------------------------------------------------------------------------------------------------

ALB_FIELDS = [
    "type", "time", "elb", "client", "target", "request_processing_time", "target_processing_time", "response_processing_time",
    "elb_status_code", "target_status_code", "received_bytes", "sent_bytes", "request", "user_agent", "ssl_cipher", "ssl_protocol",
    "target_group_arn", "trace_id", "domain_name", "chosen_cert_arn", "matched_rule_priority", "request_creation_time",
    "actions_executed", "redirect_url", "error_reason", "target_port_list", "target_status_code_list", "classification",
    "classification_reason",
]


def parse_alb(line):
    fields = split_fields(line)
    if len(fields) < 13:
        return None
    doc = dict(zip(ALB_FIELDS, fields))
    request = doc.get("request", "")
    status = doc.get("elb_status_code")
    doc["timestamp"] = doc["time"]
    doc["message"] = f"{request} {status}".strip() if request else f"{doc['type']} {status}"
    doc["level"] = _level(status)
    doc["host"] = doc.get("elb", "")
    doc["aws_format"] = "alb"
    return doc


def parse_cloudfront(line, header):
    values = line.split("\t")
    if not header or len(values) < 8:
        return None
    doc = dict(zip(header, values))
    status = doc.get("sc-status")
    doc["timestamp"] = f"{doc.get('date', '')}T{doc.get('time', '')}Z"
    doc["message"] = f"{doc.get('cs-method', '')} {doc.get('cs-uri-stem', '')} {status}".strip()
    doc["level"] = _level(status)
    doc["host"] = doc.get("x-edge-location", "")
    doc["aws_format"] = "cloudfront"
    return doc


S3_ACCESS_FIELDS = [
    "bucket_owner", "bucket", "time", "remote_ip", "requester", "request_id", "operation", "key", "request_uri", "http_status",
    "error_code", "bytes_sent", "object_size", "total_time", "turn_around_time", "referrer", "user_agent", "version_id",
    "host_id", "signature_version", "cipher_suite", "authentication_type", "host_header", "tls_version",
]


def parse_s3_access(line):
    fields = split_fields(line)
    if len(fields) < 10:
        return None
    doc = dict(zip(S3_ACCESS_FIELDS, fields))
    status = doc.get("http_status")
    # [06/Feb/2019:00:00:38 +0000]
    doc["timestamp"] = doc.get("time", "")
    doc["message"] = f"{doc.get('operation', '')} {doc.get('bucket', '')}/{doc.get('key', '')} {status}".strip()
    doc["level"] = _level(status)
    doc["host"] = doc.get("remote_ip", "")
    doc["aws_format"] = "s3-access"
    return doc


def parse_vpc_flow(line, header):
    values = line.split()
    if not header or len(values) < len(header) // 2:
        return None
    doc = dict(zip(header, values))
    action = doc.get("action", "")
    protocol = {"6": "TCP", "17": "UDP", "1": "ICMP"}.get(doc.get("protocol", ""), doc.get("protocol", ""))
    src = f"{doc.get('srcaddr', '')}:{doc.get('srcport', '')}"
    dst = f"{doc.get('dstaddr', '')}:{doc.get('dstport', '')}"
    doc["timestamp"] = doc.get("start", "")
    doc["message"] = f"{action} {protocol} {src} -> {dst} ({doc.get('bytes', '0')} bytes)"
    doc["level"] = "warning" if action == "REJECT" else "info"
    doc["host"] = doc.get("interface-id", "")
    doc["aws_format"] = "vpc-flow"
    return doc


def _flow_header(first_line):
    names = [name.replace("_", "-") for name in first_line.split()]
    return names if "srcaddr" in names and "dstaddr" in names else None


def _is_s3_access(line):
    return re.match(r"^[0-9a-f]{20,64} \S+ \[", line) is not None


def _is_alb(line):
    return re.match(r"^(http|https|h2|grpcs|ws|wss|tls|udp|tcp) \d{4}-\d\d-\d\dT", line) is not None


def documents(text):
    """Yields one document (a dict, or a plain string) per log line or JSON document of an object's text."""
    stripped = text.lstrip()

    # CloudTrail: one JSON document {"Records": [...]}.
    if stripped.startswith("{") and '"Records"' in stripped[:200]:
        try:
            for record in json.loads(stripped).get("Records", []):
                yield record
            return
        except ValueError:
            pass

    cloudfront_header = None
    flow_header = None
    first = True

    for line in io.StringIO(text):
        line = line.rstrip("\r\n")
        if not line:
            continue

        if len(line) > MAX_LINE_BYTES:
            line = line[:MAX_LINE_BYTES]

        if line.startswith("#Fields:"):
            cloudfront_header = line[len("#Fields:"):].split()
            continue
        if line.startswith("#"):
            continue

        if line.startswith("{"):
            try:
                yield json.loads(line)
                continue
            except ValueError:
                pass

        if first:
            first = False
            header = _flow_header(line)
            if header:
                flow_header = header
                continue

        doc = None
        if cloudfront_header:
            doc = parse_cloudfront(line, cloudfront_header)
        elif flow_header:
            doc = parse_vpc_flow(line, flow_header)
        elif _is_alb(line):
            doc = parse_alb(line)
        elif _is_s3_access(line):
            doc = parse_s3_access(line)

        yield doc if doc is not None else line


# --- sending ----------------------------------------------------------------------------------------------------------------


def _record(doc):
    data = doc if isinstance(doc, str) else json.dumps(doc, separators=(",", ":"))
    return {"data": base64.b64encode(data.encode("utf-8", "replace")).decode("ascii")}, len(data)


def batches(docs):
    """Groups documents into Firehose-sized deliveries."""
    batch, size = [], 0
    for doc in docs:
        record, length = _record(doc)
        if batch and (len(batch) >= MAX_BATCH_RECORDS or size + length > MAX_BATCH_BYTES):
            yield batch
            batch, size = [], 0
        batch.append(record)
        size += length
    if batch:
        yield batch


def request_id(bucket, key, etag, number):
    # The same object and batch always carry the same id, so a retried invocation is not stored twice (Tailcove remembers it for a day).
    return hashlib.sha256(f"{bucket}|{key}|{etag}|{number}".encode()).hexdigest()


def post(batch, rid, base_url, token, attempts=5):
    body = json.dumps({"requestId": rid, "timestamp": int(time.time() * 1000), "records": batch}).encode()
    request = urllib.request.Request(
        base_url.rstrip("/") + "/api/v1/aws/firehose",
        data=body,
        headers={"Content-Type": "application/json", "X-Amz-Firehose-Access-Key": token, "X-Amz-Firehose-Request-Id": rid},
        method="POST",
    )
    for attempt in range(attempts):
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                return response.status
        except urllib.error.HTTPError as error:
            # 401, 400 and 413 will not change by trying again; a rate limit or a server error may.
            if error.code not in (429, 500, 502, 503, 504) or attempt == attempts - 1:
                raise
            time.sleep(min(30, float(error.headers.get("Retry-After") or 2 ** attempt)))
        except urllib.error.URLError:
            if attempt == attempts - 1:
                raise
            time.sleep(2 ** attempt)


def read_object(s3, bucket, key):
    obj = s3.get_object(Bucket=bucket, Key=key)
    body = obj["Body"].read()
    if key.endswith(".gz") or body[:2] == b"\x1f\x8b":
        body = gzip.decompress(body)
    return obj.get("ETag", "").strip('"'), body.decode("utf-8", "replace")


def handler(event, context=None):
    import boto3

    s3 = boto3.client("s3")
    base_url, token = os.environ["TAILCOVE_URL"], os.environ["TAILCOVE_TOKEN"]
    sent = 0

    for record in event.get("Records", []):
        bucket = record["s3"]["bucket"]["name"]
        key = unquote_plus(record["s3"]["object"]["key"])
        etag, text = read_object(s3, bucket, key)

        for number, batch in enumerate(batches(documents(text))):
            post(batch, request_id(bucket, key, etag, number), base_url, token)
            sent += len(batch)

    return {"sent": sent}
