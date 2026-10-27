terraform {
  required_providers {
    tailcove = {
      source = "tailcove/tailcove"
    }
  }
}

# The token comes from TAILCOVE_TOKEN. It needs sources:write, monitors:write, alerts:write and oncall:read.
provider "tailcove" {}

data "tailcove_escalation_policy" "primary" {
  name = "Primary on-call"
}

resource "tailcove_source" "web" {
  name           = "web"
  platform       = "laravel"
  retention_days = 14
}

output "web_ingest_token" {
  value     = tailcove_source.web.ingest_token
  sensitive = true
}

resource "tailcove_monitor" "site" {
  name                 = "Marketing site"
  url                  = "https://example.com"
  method               = "GET"
  expected_status      = 200
  interval_seconds     = 60
  timeout_ms           = 10000
  create_incident      = true
  escalation_policy_id = data.tailcove_escalation_policy.primary.id

  channels = {
    mail  = { to = ["ops@example.com"] }
    slack = { webhook_url = var.slack_webhook_url }
  }
}

variable "slack_webhook_url" {
  type      = string
  sensitive = true
}

resource "tailcove_alert" "errors" {
  source_id        = tailcove_source.web.id
  name             = "Too many errors"
  query            = "level:error"
  threshold_count  = 50
  window_minutes   = 5
  cooldown_minutes = 30

  channels = {
    pagerduty = { routing_key = var.pagerduty_routing_key }
  }
}

variable "pagerduty_routing_key" {
  type      = string
  sensitive = true
}
