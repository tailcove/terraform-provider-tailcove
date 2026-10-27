# Sends an AWS account's logs, metrics and events to Tailcove, and manages the source they go to and an alert on them.
#
#   export TAILCOVE_TOKEN=...      # a Tailcove API token (sources:write, alerts:write)
#   terraform init && terraform apply

terraform {
  required_providers {
    tailcove = { source = "tailcove/tailcove" }
    aws      = { source = "hashicorp/aws" }
  }
}

provider "tailcove" {}

provider "aws" {
  region = "us-east-1"
}

resource "tailcove_source" "aws" {
  name = "aws-production"
}

module "tailcove_aws" {
  source = "../../modules/aws"

  ingest_token = tailcove_source.aws.ingest_token

  # Every Lambda function, ECS service and RDS instance that writes a log group.
  log_group_name_prefixes = ["/aws/lambda/", "/aws/ecs/", "/aws/rds/"]

  # CloudWatch metrics for the services that matter (each metric and dimension set is a series).
  metric_stream_enabled = true
  metric_namespaces     = ["AWS/EC2", "AWS/Lambda", "AWS/RDS", "AWS/ApplicationELB"]

  # GuardDuty findings, ECS and EC2 state changes, Health events and the rest of the account's EventBridge events.
  eventbridge_enabled = true

  # Logs that AWS services write to S3: a load balancer, CloudFront, VPC flow logs, CloudTrail, S3 access logs, WAF.
  s3_buckets = [
    { name = "my-alb-logs", prefix = "AWSLogs/" },
    { name = "my-cloudtrail-logs", prefix = "AWSLogs/" },
  ]
}
