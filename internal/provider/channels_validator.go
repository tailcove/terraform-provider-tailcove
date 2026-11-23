package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// atLeastOneChannel refuses a `channels` block with nothing in it: the API would, too, but only at apply time.
type atLeastOneChannel struct{}

func (atLeastOneChannel) Description(context.Context) string {
	return "at least one channel must be set"
}

func (v atLeastOneChannel) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (atLeastOneChannel) ValidateObject(_ context.Context, req validator.ObjectRequest, resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for _, attr := range req.ConfigValue.Attributes() {
		if attr.IsUnknown() {
			return
		}
		if !attr.IsNull() {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(req.Path, "No channel", "Set at least one of mail, sms, slack, discord, teams, webhook, github, telegram, pagerduty or opsgenie.")
}

// mustBeTrue keeps `members = false` out: a member channel is on or absent, never configured as off.
type mustBeTrue struct{}

func (mustBeTrue) Description(context.Context) string { return "must be true" }

func (v mustBeTrue) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (mustBeTrue) ValidateBool(_ context.Context, req validator.BoolRequest, resp *validator.BoolResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || req.ConfigValue.ValueBool() {
		return
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Members must be true", "Remove the channel to turn it off; who gets it is each member's own switch in their Tailcove profile.")
}
