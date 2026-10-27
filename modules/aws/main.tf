# Sends an AWS account's logs, metrics and events to Tailcove.
#
#   CloudWatch Logs  --subscription filter-->  Amazon Data Firehose  --HTTP endpoint-->  Tailcove /api/v1/aws/firehose
#   CloudWatch metrics --metric stream------>  (the same delivery stream)
#   EventBridge events --rule target-------->  (the same delivery stream)
#   S3 log buckets  --notification-->  Lambda (forwarder)  --HTTP-->  Tailcove /api/v1/aws/firehose
#
# One delivery stream carries logs, metrics and events: Tailcove tells them apart by what each document looks like.

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
data "aws_partition" "current" {}

data "aws_cloudwatch_log_groups" "prefixed" {
  for_each              = toset(var.log_group_name_prefixes)
  log_group_name_prefix = each.value
}

locals {
  account_id = data.aws_caller_identity.current.account_id
  region     = data.aws_region.current.name
  partition  = data.aws_partition.current.partition

  log_groups = toset(concat(
    var.log_group_names,
    flatten([for d in data.aws_cloudwatch_log_groups.prefixed : d.log_group_names]),
  ))

  s3_buckets = { for b in var.s3_buckets : b.name => b }

  tags = merge({ "managed-by" = "terraform", "tailcove" = "ingest" }, var.tags)
}

# --- The deliveries Tailcove could not take -------------------------------------------------------------------------------

resource "aws_s3_bucket" "backup" {
  bucket = "${var.name_prefix}-firehose-backup-${local.account_id}-${local.region}"
  tags   = local.tags
}

resource "aws_s3_bucket_public_access_block" "backup" {
  bucket                  = aws_s3_bucket.backup.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "backup" {
  bucket = aws_s3_bucket.backup.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "backup" {
  bucket = aws_s3_bucket.backup.id

  rule {
    id     = "expire"
    status = "Enabled"

    filter {}

    expiration {
      days = var.backup_retention_days
    }
  }
}

# --- The delivery stream --------------------------------------------------------------------------------------------------

data "aws_iam_policy_document" "firehose_trust" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["firehose.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "sts:ExternalId"
      values   = [local.account_id]
    }
  }
}

data "aws_iam_policy_document" "firehose" {
  statement {
    actions = [
      "s3:AbortMultipartUpload",
      "s3:GetBucketLocation",
      "s3:GetObject",
      "s3:ListBucket",
      "s3:ListBucketMultipartUploads",
      "s3:PutObject",
    ]
    resources = [aws_s3_bucket.backup.arn, "${aws_s3_bucket.backup.arn}/*"]
  }
}

resource "aws_iam_role" "firehose" {
  name               = "${var.name_prefix}-firehose"
  assume_role_policy = data.aws_iam_policy_document.firehose_trust.json
  tags               = local.tags
}

resource "aws_iam_role_policy" "firehose" {
  name   = "backup-bucket"
  role   = aws_iam_role.firehose.id
  policy = data.aws_iam_policy_document.firehose.json
}

resource "aws_kinesis_firehose_delivery_stream" "tailcove" {
  name        = "${var.name_prefix}-ingest"
  destination = "http_endpoint"
  tags        = local.tags

  http_endpoint_configuration {
    url                = "${var.ingest_url}/api/v1/aws/firehose"
    name               = "Tailcove"
    access_key         = var.ingest_token
    role_arn           = aws_iam_role.firehose.arn
    buffering_size     = var.firehose_buffer_mb
    buffering_interval = var.firehose_buffer_seconds
    retry_duration     = 300
    s3_backup_mode     = "FailedDataOnly"

    request_configuration {
      content_encoding = "NONE"
    }

    s3_configuration {
      role_arn           = aws_iam_role.firehose.arn
      bucket_arn         = aws_s3_bucket.backup.arn
      buffering_size     = 5
      buffering_interval = 300
      compression_format = "GZIP"
    }
  }

  depends_on = [aws_iam_role_policy.firehose]
}

# What each source of data is allowed to do: put records on the delivery stream, and nothing else.
data "aws_iam_policy_document" "put_record" {
  statement {
    actions   = ["firehose:PutRecord", "firehose:PutRecordBatch"]
    resources = [aws_kinesis_firehose_delivery_stream.tailcove.arn]
  }
}

# --- CloudWatch Logs ------------------------------------------------------------------------------------------------------

data "aws_iam_policy_document" "logs_trust" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["logs.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [local.account_id]
    }
  }
}

resource "aws_iam_role" "logs" {
  count              = length(local.log_groups) > 0 ? 1 : 0
  name               = "${var.name_prefix}-logs-to-firehose"
  assume_role_policy = data.aws_iam_policy_document.logs_trust.json
  tags               = local.tags
}

resource "aws_iam_role_policy" "logs" {
  count  = length(local.log_groups) > 0 ? 1 : 0
  name   = "put-record"
  role   = aws_iam_role.logs[0].id
  policy = data.aws_iam_policy_document.put_record.json
}

resource "aws_cloudwatch_log_subscription_filter" "tailcove" {
  for_each        = local.log_groups
  name            = "${var.name_prefix}-ingest"
  log_group_name  = each.value
  filter_pattern  = var.log_filter_pattern
  destination_arn = aws_kinesis_firehose_delivery_stream.tailcove.arn
  role_arn        = aws_iam_role.logs[0].arn

  depends_on = [aws_iam_role_policy.logs]
}

# --- CloudWatch metrics ---------------------------------------------------------------------------------------------------

data "aws_iam_policy_document" "metrics_trust" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["streams.metrics.cloudwatch.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "metrics" {
  count              = var.metric_stream_enabled ? 1 : 0
  name               = "${var.name_prefix}-metric-stream"
  assume_role_policy = data.aws_iam_policy_document.metrics_trust.json
  tags               = local.tags
}

resource "aws_iam_role_policy" "metrics" {
  count  = var.metric_stream_enabled ? 1 : 0
  name   = "put-record"
  role   = aws_iam_role.metrics[0].id
  policy = data.aws_iam_policy_document.put_record.json
}

resource "aws_cloudwatch_metric_stream" "tailcove" {
  count         = var.metric_stream_enabled ? 1 : 0
  name          = "${var.name_prefix}-metrics"
  role_arn      = aws_iam_role.metrics[0].arn
  firehose_arn  = aws_kinesis_firehose_delivery_stream.tailcove.arn
  output_format = "json"
  tags          = local.tags

  dynamic "include_filter" {
    for_each = toset(var.metric_namespaces)

    content {
      namespace = include_filter.value
    }
  }

  depends_on = [aws_iam_role_policy.metrics]
}

# --- EventBridge ----------------------------------------------------------------------------------------------------------

data "aws_iam_policy_document" "events_trust" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "events" {
  count              = var.eventbridge_enabled ? 1 : 0
  name               = "${var.name_prefix}-events-to-firehose"
  assume_role_policy = data.aws_iam_policy_document.events_trust.json
  tags               = local.tags
}

resource "aws_iam_role_policy" "events" {
  count  = var.eventbridge_enabled ? 1 : 0
  name   = "put-record"
  role   = aws_iam_role.events[0].id
  policy = data.aws_iam_policy_document.put_record.json
}

resource "aws_cloudwatch_event_rule" "tailcove" {
  count       = var.eventbridge_enabled ? 1 : 0
  name        = "${var.name_prefix}-events"
  description = "Sends events to Tailcove"
  tags        = local.tags

  event_pattern = coalesce(var.eventbridge_pattern, jsonencode({ account = [local.account_id] }))
}

resource "aws_cloudwatch_event_target" "tailcove" {
  count     = var.eventbridge_enabled ? 1 : 0
  rule      = aws_cloudwatch_event_rule.tailcove[0].name
  target_id = "tailcove"
  arn       = aws_kinesis_firehose_delivery_stream.tailcove.arn
  role_arn  = aws_iam_role.events[0].arn

  depends_on = [aws_iam_role_policy.events]
}

# --- S3 log buckets: load balancers, CloudFront, VPC flow logs, S3 access logs, CloudTrail, WAF ----------------------------------

data "archive_file" "forwarder" {
  count       = length(local.s3_buckets) > 0 ? 1 : 0
  type        = "zip"
  source_file = "${path.module}/lambda/forwarder.py"
  output_path = "${path.module}/.build/forwarder.zip"
}

data "aws_iam_policy_document" "lambda_trust" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "forwarder" {
  count = length(local.s3_buckets) > 0 ? 1 : 0

  statement {
    actions   = ["s3:GetObject"]
    resources = [for b in local.s3_buckets : "arn:${local.partition}:s3:::${b.name}/${b.prefix}*"]
  }
}

resource "aws_iam_role" "forwarder" {
  count              = length(local.s3_buckets) > 0 ? 1 : 0
  name               = "${var.name_prefix}-s3-forwarder"
  assume_role_policy = data.aws_iam_policy_document.lambda_trust.json
  tags               = local.tags
}

resource "aws_iam_role_policy_attachment" "forwarder_logs" {
  count      = length(local.s3_buckets) > 0 ? 1 : 0
  role       = aws_iam_role.forwarder[0].name
  policy_arn = "arn:${local.partition}:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy" "forwarder" {
  count  = length(local.s3_buckets) > 0 ? 1 : 0
  name   = "read-log-buckets"
  role   = aws_iam_role.forwarder[0].id
  policy = data.aws_iam_policy_document.forwarder[0].json
}

resource "aws_lambda_function" "forwarder" {
  count            = length(local.s3_buckets) > 0 ? 1 : 0
  function_name    = "${var.name_prefix}-s3-forwarder"
  role             = aws_iam_role.forwarder[0].arn
  runtime          = "python3.12"
  handler          = "forwarder.handler"
  filename         = data.archive_file.forwarder[0].output_path
  source_code_hash = data.archive_file.forwarder[0].output_base64sha256
  timeout          = 300
  memory_size      = 256
  tags             = local.tags

  environment {
    variables = {
      TAILCOVE_URL   = var.ingest_url
      TAILCOVE_TOKEN = var.ingest_token
    }
  }
}

resource "aws_lambda_permission" "s3" {
  for_each       = local.s3_buckets
  statement_id   = "AllowS3-${substr(sha1(each.key), 0, 8)}"
  action         = "lambda:InvokeFunction"
  function_name  = aws_lambda_function.forwarder[0].function_name
  principal      = "s3.amazonaws.com"
  source_arn     = "arn:${local.partition}:s3:::${each.key}"
  source_account = local.account_id
}

resource "aws_s3_bucket_notification" "forwarder" {
  for_each = local.s3_buckets
  bucket   = each.key

  lambda_function {
    lambda_function_arn = aws_lambda_function.forwarder[0].arn
    events              = ["s3:ObjectCreated:*"]
    filter_prefix       = each.value.prefix == "" ? null : each.value.prefix
    filter_suffix       = each.value.suffix == "" ? null : each.value.suffix
  }

  depends_on = [aws_lambda_permission.s3]
}
