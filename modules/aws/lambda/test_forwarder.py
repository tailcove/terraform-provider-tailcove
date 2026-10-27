import base64
import gzip
import json
import unittest

import forwarder


def decode(batch):
    return [base64.b64decode(r["data"]).decode() for r in batch]


ALB = ('https 2026-06-01T12:00:00.123456Z app/my-alb/50dc6c495c0c9188 203.0.113.9:46532 10.0.0.5:80 0.001 0.002 0.000 502 502 34 366 '
       '"GET https://example.com:443/api/users HTTP/1.1" "curl/8.1" ECDHE-RSA-AES128-GCM-SHA256 TLSv1.2 '
       'arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/tg/abc "Root=1-abc-def" "example.com" "arn:aws:acm:us-east-1:123:certificate/x" 0 '
       '2026-06-01T12:00:00.122000Z "forward" "-" "-" "10.0.0.5:80" "502" "-" "-"')

CLOUDFRONT = "#Version: 1.0\n#Fields: date time x-edge-location sc-bytes c-ip cs-method cs(Host) cs-uri-stem sc-status cs(Referer)\n2026-06-01\t12:00:00\tIAD89-C1\t1043\t203.0.113.9\tGET\td1.cloudfront.net\t/index.html\t404\t-\n"

S3_ACCESS = ('79a59df900b949e55d96a1e698fbacedfd6e09d98eacf8f8d5218e7cd47ef2be awsexamplebucket1 [06/Feb/2019:00:00:38 +0000] 192.0.2.3 '
             '79a59df900b949e55d96a1e698fbacedfd6e09d98eacf8f8d5218e7cd47ef2be 3E57427F3EXAMPLE REST.GET.OBJECT a/b.png '
             '"GET /awsexamplebucket1/a/b.png HTTP/1.1" 200 - 113 113 7 6 "-" "curl/7.15.1" -')

FLOW = ("version account-id interface-id srcaddr dstaddr srcport dstport protocol packets bytes start end action log-status\n"
        "2 123456789012 eni-1 10.0.0.1 10.0.0.2 443 5000 6 10 840 1780317000 1780317060 REJECT OK\n")


class Parsing(unittest.TestCase):
    def test_alb_line_becomes_a_document_with_a_summary_and_a_level(self):
        doc = next(forwarder.documents(ALB + "\n"))
        self.assertEqual(doc["aws_format"], "alb")
        self.assertEqual(doc["message"], "GET https://example.com:443/api/users HTTP/1.1 502")
        self.assertEqual(doc["level"], "error")
        self.assertEqual(doc["timestamp"], "2026-06-01T12:00:00.123456Z")
        self.assertEqual(doc["client"], "203.0.113.9:46532")
        self.assertEqual(doc["host"], "app/my-alb/50dc6c495c0c9188")

    def test_cloudfront_uses_the_fields_header(self):
        doc = next(forwarder.documents(CLOUDFRONT))
        self.assertEqual(doc["aws_format"], "cloudfront")
        self.assertEqual(doc["message"], "GET /index.html 404")
        self.assertEqual(doc["level"], "warning")
        self.assertEqual(doc["timestamp"], "2026-06-01T12:00:00Z")

    def test_s3_access_log_keeps_the_bracketed_time_and_the_quoted_request(self):
        doc = next(forwarder.documents(S3_ACCESS + "\n"))
        self.assertEqual(doc["aws_format"], "s3-access")
        self.assertEqual(doc["time"], "06/Feb/2019:00:00:38 +0000")
        self.assertEqual(doc["request_uri"], "GET /awsexamplebucket1/a/b.png HTTP/1.1")
        self.assertEqual(doc["message"], "REST.GET.OBJECT awsexamplebucket1/a/b.png 200")

    def test_vpc_flow_reads_its_header_and_warns_on_reject(self):
        docs = list(forwarder.documents(FLOW))
        self.assertEqual(len(docs), 1)
        self.assertEqual(docs[0]["aws_format"], "vpc-flow")
        self.assertEqual(docs[0]["message"], "REJECT TCP 10.0.0.1:443 -> 10.0.0.2:5000 (840 bytes)")
        self.assertEqual(docs[0]["level"], "warning")
        self.assertEqual(docs[0]["timestamp"], "1780317000")

    def test_cloudtrail_object_yields_each_record(self):
        text = json.dumps({"Records": [{"eventName": "GetObject", "eventSource": "s3.amazonaws.com"}, {"eventName": "PutObject", "eventSource": "s3.amazonaws.com"}]})
        self.assertEqual([d["eventName"] for d in forwarder.documents(text)], ["GetObject", "PutObject"])

    def test_json_lines_and_plain_text_pass_through(self):
        docs = list(forwarder.documents('{"a": 1}\nnot json\n\n{"b": 2}\n'))
        self.assertEqual(docs, [{"a": 1}, "not json", {"b": 2}])

    def test_a_line_that_is_not_in_a_known_format_stays_text(self):
        self.assertEqual(list(forwarder.documents("https but not an alb line\n")), ["https but not an alb line"])

    def test_a_huge_line_is_cut(self):
        doc = next(forwarder.documents("x" * (forwarder.MAX_LINE_BYTES * 2)))
        self.assertEqual(len(doc), forwarder.MAX_LINE_BYTES)


class Sending(unittest.TestCase):
    def test_batches_respect_the_record_cap(self):
        docs = [{"n": i} for i in range(forwarder.MAX_BATCH_RECORDS * 2 + 1)]
        sizes = [len(b) for b in forwarder.batches(docs)]
        self.assertEqual(sizes, [forwarder.MAX_BATCH_RECORDS, forwarder.MAX_BATCH_RECORDS, 1])

    def test_batches_respect_the_byte_cap(self):
        docs = ["y" * (forwarder.MAX_BATCH_BYTES // 2 + 1) for _ in range(3)]
        self.assertEqual([len(b) for b in forwarder.batches(docs)], [1, 1, 1])

    def test_records_are_base64_of_the_json(self):
        batch = next(forwarder.batches([{"a": 1}, "text"]))
        self.assertEqual(decode(batch), ['{"a":1}', "text"])

    def test_the_request_id_is_stable_for_the_same_object_and_batch_and_differs_otherwise(self):
        a = forwarder.request_id("b", "k", "etag", 0)
        self.assertEqual(a, forwarder.request_id("b", "k", "etag", 0))
        self.assertNotEqual(a, forwarder.request_id("b", "k", "etag", 1))
        self.assertNotEqual(a, forwarder.request_id("b", "k", "other", 0))
        self.assertLessEqual(len(a), 128)

    def test_gzip_objects_are_read(self):
        class Body:
            def read(self):
                return gzip.compress(b"hello\n")

        class S3:
            def get_object(self, Bucket, Key):
                return {"Body": Body(), "ETag": '"abc"'}

        self.assertEqual(forwarder.read_object(S3(), "b", "k.gz"), ("abc", "hello\n"))


if __name__ == "__main__":
    unittest.main()
