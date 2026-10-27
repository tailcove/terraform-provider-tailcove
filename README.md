<a href="https://tailcove.dev/docs/terraform"><picture><source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/tailcove/.github/main/profile/assets/packages/terraform-dark.svg"><source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/tailcove/.github/main/profile/assets/packages/terraform-light.svg"><img alt="terraform-provider-tailcove: Tailcove for Terraform. Version, runtime support, size and license." src="https://raw.githubusercontent.com/tailcove/.github/main/profile/assets/packages/terraform-dark.svg" width="100%"></picture></a>

<p align="center">
  <a href="https://tailcove.dev/docs/terraform">Documentation</a> &nbsp;·&nbsp;
  <a href="https://app.tailcove.dev/register">Get started free</a> &nbsp;·&nbsp;
  <a href="https://github.com/tailcove/terraform-provider-tailcove/releases">View releases</a> &nbsp;·&nbsp;
  <a href="https://github.com/tailcove/terraform-provider-tailcove/issues/new/choose">Report a bug</a>
</p>

<h3 align="center">Manage Tailcove sources, monitors and alerts as code.</h3>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/tailcove/.github/main/profile/assets/packages/features-terraform-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/tailcove/.github/main/profile/assets/packages/features-terraform-light.svg">
  <img alt="terraform-provider-tailcove features" src="https://raw.githubusercontent.com/tailcove/.github/main/profile/assets/packages/features-terraform-dark.svg" width="100%">
</picture>

## Install

The provider is not in the Terraform Registry yet (that needs a GPG-signed checksums file). Until it is, download the
archive for your platform from the [releases page](https://github.com/tailcove/terraform-provider-tailcove/releases),
check it against `..._SHA256SUMS`, and point Terraform at the binary with a `dev_overrides` block (see
[Development](#development)) or a filesystem mirror.

```hcl
terraform {
  required_providers {
    tailcove = { source = "tailcove/tailcove" }
  }
}

provider "tailcove" {}   # token from TAILCOVE_TOKEN
```

## Quick start

```hcl
resource "tailcove_source" "web" {
  name = "web"
}

resource "tailcove_monitor" "site" {
  name             = "Marketing site"
  url              = "https://example.com"
  method           = "GET"
  expected_status  = 200
  interval_seconds = 60
  timeout_ms       = 10000
  channels = { mail = { to = ["ops@example.com"] } }
}
```

A full example is in [`examples/complete`](examples/complete/main.tf).

## Provider

- `endpoint`: environment variable `TAILCOVE_ENDPOINT`, default `https://app.tailcove.dev`.
- `token`: environment variable `TAILCOVE_TOKEN`, required.

The token is an API token from **Settings, API tokens**. It acts for the team it was made for, as its owner, so one
provider configuration manages one team (use `alias` for more). Give it only the abilities the configuration uses:
`sources:write`, `monitors:write`, `alerts:write`, and `oncall:read` for the data sources. A write needs the matching
`:write` ability, and a `:write` does not include the `:read`; Terraform reads what it manages, so a token that manages
sources needs `sources:read` as well.

## Resources and data sources

- `tailcove_source`: resource, imported by `id`.
- `tailcove_monitor`: resource, imported by `id`.
- `tailcove_alert`: resource, imported by `source_id/alert_id`.
- `tailcove_source`: data source, found by `id` or `name`.
- `tailcove_escalation_policy`: data source, found by `name`.
- `tailcove_oncall_schedule`: data source, found by `name`.

Every attribute is described in the schema (`terraform providers schema -json`, or the editor's hover help).

### What is not here, and why

The provider builds on the write API (`/api/v1`). That API can create and change sources, monitors and alerts, and can
only read status pages, escalation policies and on-call schedules (it edits status-page announcements, incidents and
on-call overrides). So **status pages, escalation policies and schedules are not resources yet**: they are edited in the
Tailcove app, and the provider offers data sources to refer to a policy or schedule by name. They become resources
when the API can write them.

### Behaviour worth knowing

- **Secrets are write-only.** Tailcove stores channel secrets (a GitHub, Telegram, PagerDuty or Opsgenie key) and
  monitor request-header values encrypted and never returns them. The provider keeps them from your configuration, so a
  change made in the app to one of these is not detected, and an imported resource has none until you set them in the
  configuration (the next plan then shows the update).
- **The ingest token** of a source is returned once, when the source is created. It is kept in the state as a sensitive
  value (`tailcove_source.x.ingest_token`), so protect the state. A source that was imported has no token.
- **Only what you set is tracked.** The API fills in defaults for fields you leave out; the provider does not show
  those as changes. Set a field to make Terraform manage it.
- **Plan limits apply.** Creating more sources or monitors than the plan allows, an interval shorter than the plan's
  floor, or more regions than the plan allows fails at apply with the API's message.
- **Retries.** A write is limited to 30 a minute per token. The provider waits and retries a throttled request, and
  every create carries an `Idempotency-Key`, so a retry never creates a second thing.
- **Deleting a source** deletes its logs and alerts. Only a token of an owner or admin may.

## Development

```
go build ./...
go test ./... -count=1
```

The tests in `internal/provider` run the real Terraform CLI against an in-memory stand-in for the API
(`fakeapi_test.go`), so they need a `terraform` binary on `PATH` (or `TF_ACC_TERRAFORM_PATH`). They cover create,
update, refresh, import, drift (a thing deleted outside Terraform), validation errors, retained secrets and the data
sources. They do not replace a run against a real Tailcove stack; the stand-in must be kept in step with the API
(`resources/docs/api` in the platform repository, and `/docs/api/openapi.json`).

To try a build without installing it, point Terraform at it in a CLI config:

```hcl
provider_installation {
  dev_overrides { "tailcove/tailcove" = "/path/to/dir/with/the/binary" }
  direct {}
}
```

## Releasing

A pushed `v*` tag runs `.github/workflows/release.yml`: the tests, then GoReleaser, which attaches the archives and
`..._SHA256SUMS` to a GitHub release (`v0.1.0-rc.0` is a pre-release). No secret is stored in the repository.

Publishing in the Terraform Registry additionally needs the checksums file signed with a GPG key whose public half is
registered in the Registry, which is a secret. That is not set up; until it is, install from a release by hand or
through a filesystem mirror.

## Help and feedback

Something wrong or missing? [Open an issue](https://github.com/tailcove/terraform-provider-tailcove/issues/new/choose).

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/tailcove/.github/main/profile/assets/packages/footer-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/tailcove/.github/main/profile/assets/packages/footer-light.svg">
  <img alt="Tailcove. Built by AKCybex." src="https://raw.githubusercontent.com/tailcove/.github/main/profile/assets/packages/footer-dark.svg" width="100%">
</picture>

<p align="center">
  <a href="https://tailcove.dev/docs/terraform">Documentation</a> &nbsp;·&nbsp;
  <a href="https://app.tailcove.dev/register">Get started free</a> &nbsp;·&nbsp;
  <a href="https://github.com/tailcove/terraform-provider-tailcove/issues/new/choose">Report a bug</a> &nbsp;·&nbsp;
  <a href="LICENSE">MIT License</a>
</p>

<p align="center">
  <sub>More Tailcove libraries: <a href="https://github.com/tailcove/laravel">Laravel</a> &nbsp;·&nbsp; <a href="https://github.com/tailcove/node">Node.js</a> &nbsp;·&nbsp; <a href="https://github.com/tailcove/python">Python</a> &nbsp;·&nbsp; <a href="https://github.com/tailcove/browser">Browser</a> &nbsp;·&nbsp; <a href="https://github.com/tailcove/agent">Agent</a></sub>
</p>
