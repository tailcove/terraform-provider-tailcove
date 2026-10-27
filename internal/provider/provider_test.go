package provider_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/tailcove/terraform-provider-tailcove/internal/provider"
)

var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"tailcove": providerserver.NewProtocol6WithError(provider.New("test")()),
}

// These tests run the real Terraform CLI against an in-memory API (fakeapi_test.go), so they need a
// terraform binary: set TF_ACC_TERRAFORM_PATH, or have terraform on PATH.
func preCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Setenv("TF_ACC", "1")
	}
}

func providerConfig(url string) string {
	return fmt.Sprintf(`
provider "tailcove" {
  endpoint = %q
  token    = "test-token"
}
`, url)
}

const slackChannel = `
  channels = {
    slack = { webhook_url = "https://hooks.slack.com/services/T000/B000/XXXX" }
  }
`

func TestSourceLifecycle(t *testing.T) {
	preCheck(t)
	api, url := newFakeAPI(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(url) + `
resource "tailcove_source" "web" {
  name     = "web"
  platform = "laravel"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("tailcove_source.web", "id"),
					resource.TestCheckResourceAttr("tailcove_source.web", "name", "web"),
					resource.TestCheckResourceAttr("tailcove_source.web", "retention_days", "3"),
					resource.TestMatchResourceAttr("tailcove_source.web", "ingest_token", regexp.MustCompile(`^tok_\d+$`)),
				),
			},
			{
				// The token is only in the create's response: it must survive a refresh and an update.
				Config: providerConfig(url) + `
resource "tailcove_source" "web" {
  name     = "web-renamed"
  platform = "laravel"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tailcove_source.web", "name", "web-renamed"),
					resource.TestMatchResourceAttr("tailcove_source.web", "ingest_token", regexp.MustCompile(`^tok_\d+$`)),
				),
			},
			{
				// Removing platform clears it: a PUT replaces, so the provider must send null.
				Config: providerConfig(url) + `
resource "tailcove_source" "web" {
  name = "web-renamed"
}`,
				Check: resource.TestCheckNoResourceAttr("tailcove_source.web", "platform"),
			},
			{
				ResourceName:            "tailcove_source.web",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"ingest_token"},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if len(api.sources) != 0 {
				return fmt.Errorf("%d sources left", len(api.sources))
			}
			return nil
		},
	})

	if got := api.bodies["PUT /api/v1/sources/101"]; got == nil || got["platform"] != nil {
		t.Errorf("the last PUT should carry platform: null, got %v", got)
	}
}

func TestSourceGoneOutsideTerraformIsRecreated(t *testing.T) {
	preCheck(t)
	api, url := newFakeAPI(t)
	cfg := providerConfig(url) + `
resource "tailcove_source" "web" { name = "web" }`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					api.mu.Lock()
					api.sources = map[int64]map[string]any{}
					api.mu.Unlock()
				},
				Config: cfg,
				Check:  resource.TestCheckResourceAttrSet("tailcove_source.web", "id"),
			},
		},
	})
}

func TestMonitorHTTPLifecycle(t *testing.T) {
	preCheck(t)
	api, url := newFakeAPI(t)

	cfg := func(interval int, extra string) string {
		return providerConfig(url) + fmt.Sprintf(`
resource "tailcove_monitor" "site" {
  name             = "site"
  url              = "https://example.com/health"
  method           = "GET"
  expected_status  = 200
  interval_seconds = %d
  timeout_ms       = 10000
  request_headers  = { "X-Token" = "s3cret" }
  %s
  channels = {
    mail  = { to = ["ops@example.com"] }
    slack = { webhook_url = "https://hooks.slack.com/services/T000/B000/XXXX" }
  }
}`, interval, extra)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: cfg(300, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tailcove_monitor.site", "type", "http"),
					resource.TestCheckResourceAttr("tailcove_monitor.site", "is_active", "true"),
					resource.TestCheckResourceAttr("tailcove_monitor.site", "create_incident", "false"),
					resource.TestCheckResourceAttr("tailcove_monitor.site", "interval_seconds", "300"),
					resource.TestCheckResourceAttr("tailcove_monitor.site", "state", "unknown"),
					resource.TestCheckResourceAttr("tailcove_monitor.site", "channels.mail.to.0", "ops@example.com"),
					resource.TestCheckNoResourceAttr("tailcove_monitor.site", "keyword"),
				),
			},
			{
				// The header value is never returned, so it must not show as a change.
				Config:   cfg(300, ""),
				PlanOnly: true,
			},
			{
				Config: cfg(60, `keyword = "ok"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tailcove_monitor.site", "interval_seconds", "60"),
					resource.TestCheckResourceAttr("tailcove_monitor.site", "keyword", "ok"),
				),
			},
			{
				ResourceName:      "tailcove_monitor.site",
				ImportState:       true,
				ImportStateVerify: true,
				// Secrets are not returned by the API, so an import cannot have them.
				ImportStateVerifyIgnore: []string{"request_headers", "channels.slack"},
			},
		},
	})

	var sent map[string]any
	for key, body := range api.bodies {
		if key == "POST /api/v1/monitors" {
			sent = body
		}
	}
	headers, _ := sent["request_headers"].([]any)
	if len(headers) != 1 || headers[0].(map[string]any)["name"] != "X-Token" || headers[0].(map[string]any)["value"] != "s3cret" {
		t.Errorf("request_headers sent = %v", sent["request_headers"])
	}
}

func TestMonitorValidationErrorIsShown(t *testing.T) {
	preCheck(t)
	_, url := newFakeAPI(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
resource "tailcove_monitor" "bad" {
  name = "bad"
  channels = { mail = { to = ["a@example.com"] } }
}`,
			ExpectError: regexp.MustCompile(`url: The url field is\s+required`),
		}},
	})
}

func TestChannelsNeedOne(t *testing.T) {
	preCheck(t)
	_, url := newFakeAPI(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
resource "tailcove_monitor" "none" {
  name     = "none"
  url      = "https://example.com"
  channels = {}
}`,
			ExpectError: regexp.MustCompile(`No channel`),
		}},
	})
}

func TestAlertLifecycleAndImport(t *testing.T) {
	preCheck(t)
	api, url := newFakeAPI(t)

	cfg := func(threshold int) string {
		return providerConfig(url) + fmt.Sprintf(`
resource "tailcove_source" "web" { name = "web" }

resource "tailcove_alert" "errors" {
  source_id        = tailcove_source.web.id
  name             = "Too many errors"
  query            = "level:error"
  threshold_count  = %d
  window_minutes   = 5
  cooldown_minutes = 30
  channels = {
    pagerduty = { routing_key = "R0123456789012345678901234567890AB" }
    mail      = { to = ["ops@example.com"] }
  }
}`, threshold)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: cfg(10),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tailcove_alert.errors", "type", "log"),
					resource.TestCheckResourceAttr("tailcove_alert.errors", "threshold_count", "10"),
					resource.TestCheckResourceAttr("tailcove_alert.errors", "is_active", "true"),
					resource.TestCheckResourceAttrPair("tailcove_alert.errors", "source_id", "tailcove_source.web", "id"),
				),
			},
			// The PagerDuty key is never returned: a refresh must keep it, not ask to set it again.
			{Config: cfg(10), PlanOnly: true},
			{Config: cfg(25), Check: resource.TestCheckResourceAttr("tailcove_alert.errors", "threshold_count", "25")},
			{
				ResourceName:      "tailcove_alert.errors",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					a := s.RootModule().Resources["tailcove_alert.errors"].Primary
					return a.Attributes["source_id"] + "/" + a.ID, nil
				},
				ImportStateVerifyIdentifierAttribute: "id",
				ImportStateVerifyIgnore:              []string{"channels.pagerduty"},
			},
		},
	})

	if len(api.alerts) != 0 {
		t.Errorf("%d alerts left after destroy", len(api.alerts))
	}
}

func TestAlertImportRejectsABadID(t *testing.T) {
	preCheck(t)
	_, url := newFakeAPI(t)
	cfg := providerConfig(url) + `
resource "tailcove_source" "web" { name = "web" }

resource "tailcove_alert" "errors" {
  source_id        = tailcove_source.web.id
  name             = "errors"
  query            = "level:error"
  threshold_count  = 1
  window_minutes   = 5
  cooldown_minutes = 5
  channels = { mail = { to = ["ops@example.com"] } }
}`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				Config:        cfg,
				ResourceName:  "tailcove_alert.errors",
				ImportState:   true,
				ImportStateId: "12",
				ExpectError:   regexp.MustCompile(`Use source_id/alert_id`),
			},
		},
	})
}

func TestDataSources(t *testing.T) {
	preCheck(t)
	api, url := newFakeAPI(t)
	api.sources[7] = map[string]any{"name": "existing", "platform": "nginx", "retention_days": 7}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: providerConfig(url) + `
data "tailcove_source" "by_name" { name = "existing" }
data "tailcove_source" "by_id"   { id = 7 }
data "tailcove_escalation_policy" "p" { name = "Primary on-call" }
data "tailcove_oncall_schedule" "s"   { name = "Weekdays" }
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.tailcove_source.by_name", "id", "7"),
				resource.TestCheckResourceAttr("data.tailcove_source.by_name", "platform", "nginx"),
				resource.TestCheckResourceAttr("data.tailcove_source.by_id", "name", "existing"),
				resource.TestCheckResourceAttr("data.tailcove_escalation_policy.p", "id", "5"),
				resource.TestCheckResourceAttr("data.tailcove_oncall_schedule.s", "time_zone", "Europe/Berlin"),
			),
		}},
	})
}

func TestDataSourceNotFound(t *testing.T) {
	preCheck(t)
	_, url := newFakeAPI(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      providerConfig(url) + `data "tailcove_escalation_policy" "p" { name = "Nope" }`,
			ExpectError: regexp.MustCompile(`no escalation policy is named "Nope"`),
		}},
	})
}

func TestMissingToken(t *testing.T) {
	preCheck(t)
	t.Setenv("TAILCOVE_TOKEN", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      `provider "tailcove" {}` + "\n" + `data "tailcove_escalation_policy" "p" { name = "x" }`,
			ExpectError: regexp.MustCompile(`Missing token`),
		}},
	})
}

func TestWrongTokenIsRefused(t *testing.T) {
	preCheck(t)
	_, url := newFakeAPI(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      fmt.Sprintf("provider \"tailcove\" {\n endpoint = %q\n token = \"nope\"\n}\n", url) + `resource "tailcove_source" "web" { name = "web" }`,
			ExpectError: regexp.MustCompile(`401`),
		}},
	})
}
