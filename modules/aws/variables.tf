variable "ingest_token" {
  description = "The ingest token of the Tailcove source the AWS data goes to (for example tailcove_source.aws.ingest_token). It is the delivery stream's access key and the forwarder's token, and ends up in the state: protect it."
  type        = string
  sensitive   = true
}

variable "ingest_url" {
  description = "The Tailcove ingest base URL, with no path."
  type        = string
  default     = "https://ingest.tailcove.dev"

  validation {
    condition     = can(regex("^https://[^/]+$", var.ingest_url))
    error_message = "Use an https URL with no path, for example https://ingest.tailcove.dev (Firehose only delivers to https on port 443)."
  }
}

variable "name_prefix" {
  description = "Prefix of every resource this module creates."
  type        = string
  default     = "tailcove"
}

# --- CloudWatch Logs: Lambda, API Gateway, ECS, EKS, RDS, anything that writes a log group -------------------------------------

variable "log_group_names" {
  description = "CloudWatch log groups to send to Tailcove, by exact name."
  type        = list(string)
  default     = []
}

variable "log_group_name_prefixes" {
  description = "Send every log group whose name starts with one of these (for example /aws/lambda/ for every function, /aws/ecs/ for ECS, /aws/rds/ for RDS), as they are when the module is applied. A log group can have at most two subscription filters in all."
  type        = list(string)
  default     = []
}

variable "log_filter_pattern" {
  description = "A CloudWatch Logs filter pattern applied to every subscribed group. Empty sends everything."
  type        = string
  default     = ""
}

# --- CloudWatch metrics -------------------------------------------------------------------------------------------------------

variable "metric_stream_enabled" {
  description = "Stream CloudWatch metrics to Tailcove. Every metric and dimension set is a series, and a plan caps its series, so choose namespaces with metric_namespaces."
  type        = bool
  default     = false
}

variable "metric_namespaces" {
  description = "The namespaces the metric stream carries, for example [\"AWS/EC2\", \"AWS/Lambda\", \"AWS/RDS\", \"AWS/ApplicationELB\"]. Empty means every namespace."
  type        = list(string)
  default     = []
}

# --- EventBridge: events from every AWS service ----------------------------------------------------------------------------

variable "eventbridge_enabled" {
  description = "Send events from the account's default event bus to Tailcove: GuardDuty findings, ECS and EC2 state changes, Health, CodePipeline, Config, Security Hub and every other service that publishes to EventBridge."
  type        = bool
  default     = false
}

variable "eventbridge_pattern" {
  description = "An EventBridge event pattern (JSON) that selects what is sent. Null sends every event of this account."
  type        = string
  default     = null
}

# --- S3: load balancers, CloudFront, VPC flow logs, S3 access logs, CloudTrail, WAF ---------------------------------------------

variable "s3_buckets" {
  description = "Buckets where AWS services write their logs. A new object is read by a small Lambda function and sent to Tailcove. This module sets the bucket's event notification, which REPLACES any notification the bucket already has."
  type = list(object({
    name   = string
    prefix = optional(string, "")
    suffix = optional(string, "")
  }))
  default = []
}

# --- Delivery stream and housekeeping -------------------------------------------------------------------------------------

variable "firehose_buffer_mb" {
  description = "How much the delivery stream collects before it delivers (MB). Tailcove takes a request of up to 5 MB, so 1 to 4."
  type        = number
  default     = 1

  validation {
    condition     = var.firehose_buffer_mb >= 1 && var.firehose_buffer_mb <= 4
    error_message = "Use 1 to 4: Tailcove refuses a request over 5 MB."
  }
}

variable "firehose_buffer_seconds" {
  description = "How long the delivery stream waits before it delivers what it has (seconds, 60 to 900)."
  type        = number
  default     = 60
}

variable "backup_retention_days" {
  description = "How long the deliveries Tailcove could not take (Firehose retries for five minutes, then keeps them here) are kept."
  type        = number
  default     = 14
}

variable "tags" {
  description = "Tags for everything that can have them."
  type        = map(string)
  default     = {}
}
