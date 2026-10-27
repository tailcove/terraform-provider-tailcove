output "delivery_stream_arn" {
  description = "The Firehose delivery stream every log, metric and event goes through. Point other producers (a Kinesis subscription, a custom application) at it."
  value       = aws_kinesis_firehose_delivery_stream.tailcove.arn
}

output "delivery_stream_name" {
  value = aws_kinesis_firehose_delivery_stream.tailcove.name
}

output "backup_bucket" {
  description = "Where deliveries Tailcove could not take are kept, for the retention you set."
  value       = aws_s3_bucket.backup.bucket
}

output "subscribed_log_groups" {
  description = "The log groups sent to Tailcove."
  value       = sort(tolist(local.log_groups))
}

output "forwarder_function_name" {
  description = "The Lambda function that sends S3-delivered logs, when s3_buckets is set."
  value       = try(aws_lambda_function.forwarder[0].function_name, null)
}
