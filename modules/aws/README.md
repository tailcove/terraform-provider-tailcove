# Tailcove for AWS

One Terraform module that sends an AWS account's logs, metrics and events to [Tailcove](https://tailcove.dev).

```hcl
module "tailcove_aws" {
  source = "github.com/tailcove/terraform-provider-tailcove//modules/aws"

  ingest_token = tailcove_source.aws.ingest_token

  log_group_name_prefixes = ["/aws/lambda/", "/aws/ecs/", "/aws/rds/"]
  metric_stream_enabled   = true
  metric_namespaces       = ["AWS/EC2", "AWS/Lambda", "AWS/RDS", "AWS/ApplicationELB"]
  eventbridge_enabled     = true
  s3_buckets              = [{ name = "my-alb-logs", prefix = "AWSLogs/" }]
}
```

A full example, with the Tailcove source and provider, is in [`examples/aws`](../../examples/aws/main.tf). The full guide is in the
[documentation](https://tailcove.dev/docs/sending-logs/aws).

## What it covers

- **CloudWatch Logs** (Lambda, API Gateway, ECS, EKS, RDS, anything that writes a log group): a subscription filter on each log group, to one Firehose delivery stream.
- **CloudWatch metrics** (every namespace, or the ones you choose): a metric stream, to the same delivery stream.
- **EventBridge events** (GuardDuty, ECS and EC2 state changes, Health, Config, Security Hub, CodePipeline and every other service that publishes events): a rule on the default event bus, to the same delivery stream.
- **Logs written to S3** (Application and Network Load Balancer, CloudFront, VPC flow logs, S3 access logs, CloudTrail, WAF): an object-created notification, to a small Lambda function that reads the file and sends it.

The delivery stream posts to Tailcove's `/api/v1/aws/firehose` endpoint, which tells logs, metrics and events apart by what each
document looks like. Deliveries Tailcove cannot take (it was down, the source was over its limit) are retried for five minutes and then
kept in a backup bucket for `backup_retention_days`.

## Things to know

- **The token is a secret in the state.** It is the delivery stream's access key and the forwarder's token. Keep the state private.
- **A log group can have two subscription filters in all.** If one already has two, subscribing it fails.
- **`s3_buckets` replaces the bucket's event notification.** A bucket can have only one notification configuration, and this module sets it.
  A bucket that already notifies something else needs that configuration merged by hand.
- **Metric streams cost money and fill series.** CloudWatch charges per metric update, and each metric and dimension set is a Tailcove
  series (a plan caps them). Choose `metric_namespaces`.
- **The module acts on one region.** Use it once per region (the AWS provider's region), with a source per region or one for all.
- The Firehose buffer is 1 MB or 60 seconds, whichever comes first: a delivery stays under Tailcove's 5 MB limit.
