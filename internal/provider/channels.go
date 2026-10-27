package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// channelsModel is the `channels` block of an alert or a monitor. At least one channel is required.
type channelsModel struct {
	Mail      *mailModel      `tfsdk:"mail"`
	Slack     *hookModel      `tfsdk:"slack"`
	Discord   *hookModel      `tfsdk:"discord"`
	Teams     *hookModel      `tfsdk:"teams"`
	Webhook   *webhookModel   `tfsdk:"webhook"`
	Github    *githubModel    `tfsdk:"github"`
	Telegram  *telegramModel  `tfsdk:"telegram"`
	Pagerduty *pagerdutyModel `tfsdk:"pagerduty"`
	Opsgenie  *opsgenieModel  `tfsdk:"opsgenie"`
}

type mailModel struct {
	To []types.String `tfsdk:"to"`
}

type hookModel struct {
	WebhookURL types.String `tfsdk:"webhook_url"`
}

type webhookModel struct {
	URL types.String `tfsdk:"url"`
}

type githubModel struct {
	Repo  types.String `tfsdk:"repo"`
	Token types.String `tfsdk:"token"`
}

type telegramModel struct {
	ChatID   types.String `tfsdk:"chat_id"`
	BotToken types.String `tfsdk:"bot_token"`
}

type pagerdutyModel struct {
	RoutingKey types.String `tfsdk:"routing_key"`
}

type opsgenieModel struct {
	APIKey types.String `tfsdk:"api_key"`
	Region types.String `tfsdk:"region"`
}

func channelsAttribute() schema.SingleNestedAttribute {
	str := func(desc string, sensitive bool) schema.StringAttribute {
		return schema.StringAttribute{Required: true, Sensitive: sensitive, Description: desc}
	}
	hook := func(desc string) schema.SingleNestedAttribute {
		return schema.SingleNestedAttribute{
			Optional:    true,
			Description: desc,
			Attributes:  map[string]schema.Attribute{"webhook_url": str("The incoming-webhook URL. Must be a public https URL.", true)},
		}
	}

	return schema.SingleNestedAttribute{
		Required:    true,
		Description: "Where a notification goes. Set at least one channel. Every URL is checked by Tailcove's outbound guard: it must be public.",
		Attributes: map[string]schema.Attribute{
			"mail": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "Email, up to 10 recipients.",
				Attributes: map[string]schema.Attribute{
					"to": schema.ListAttribute{Required: true, ElementType: types.StringType, Description: "Recipient addresses."},
				},
			},
			"slack":   hook("Slack incoming webhook."),
			"discord": hook("Discord webhook."),
			"teams":   hook("Microsoft Teams webhook (https)."),
			"webhook": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "A generic webhook that receives a JSON POST.",
				Attributes:  map[string]schema.Attribute{"url": str("The URL to POST to.", true)},
			},
			"github": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "Opens a GitHub issue.",
				Attributes: map[string]schema.Attribute{
					"repo":  str("owner/name of the repository.", false),
					"token": str("A token that may open issues there. Stored encrypted and never returned.", true),
				},
			},
			"telegram": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "A Telegram chat.",
				Attributes: map[string]schema.Attribute{
					"chat_id":   str("The chat id.", false),
					"bot_token": str("The bot token. Stored encrypted and never returned.", true),
				},
			},
			"pagerduty": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "PagerDuty Events API v2.",
				Attributes:  map[string]schema.Attribute{"routing_key": str("The integration's routing key. Stored encrypted and never returned.", true)},
			},
			"opsgenie": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "Opsgenie.",
				Attributes: map[string]schema.Attribute{
					"api_key": str("The integration's API key. Stored encrypted and never returned.", true),
					"region":  str("us or eu.", false),
				},
			},
		},
		Validators: []validator.Object{atLeastOneChannel{}},
	}
}

// toAPI is the `channels` object the API expects.
func (c channelsModel) toAPI() map[string]any {
	out := map[string]any{}
	if c.Mail != nil {
		to := make([]string, 0, len(c.Mail.To))
		for _, a := range c.Mail.To {
			to = append(to, a.ValueString())
		}
		out["mail"] = map[string]any{"to": to}
	}
	if c.Slack != nil {
		out["slack"] = map[string]any{"webhook_url": c.Slack.WebhookURL.ValueString()}
	}
	if c.Discord != nil {
		out["discord"] = map[string]any{"webhook_url": c.Discord.WebhookURL.ValueString()}
	}
	if c.Teams != nil {
		out["teams"] = map[string]any{"webhook_url": c.Teams.WebhookURL.ValueString()}
	}
	if c.Webhook != nil {
		out["webhook"] = map[string]any{"url": c.Webhook.URL.ValueString()}
	}
	if c.Github != nil {
		out["github"] = map[string]any{"repo": c.Github.Repo.ValueString(), "token": c.Github.Token.ValueString()}
	}
	if c.Telegram != nil {
		out["telegram"] = map[string]any{"chat_id": c.Telegram.ChatID.ValueString(), "bot_token": c.Telegram.BotToken.ValueString()}
	}
	if c.Pagerduty != nil {
		out["pagerduty"] = map[string]any{"routing_key": c.Pagerduty.RoutingKey.ValueString()}
	}
	if c.Opsgenie != nil {
		out["opsgenie"] = map[string]any{"api_key": c.Opsgenie.APIKey.ValueString(), "region": c.Opsgenie.Region.ValueString()}
	}
	return out
}

// refresh brings the channels in line with what the API holds, for drift detection. A secret is never
// returned, so it stays as it is in the state; a channel the API no longer has is dropped, and one the
// configuration never had is only taken while importing (its secrets are then left for the next plan to ask for).
func (c *channelsModel) refresh(api map[string]any, importing bool) {
	has := func(name string) (map[string]any, bool) {
		m, ok := api[name].(map[string]any)
		return m, ok
	}
	str := func(m map[string]any, key string) types.String {
		if s, ok := m[key].(string); ok {
			return types.StringValue(s)
		}
		return types.StringNull()
	}

	if m, ok := has("mail"); ok && (importing || c.Mail != nil) {
		var to []types.String
		if list, ok := m["to"].([]any); ok {
			for _, a := range list {
				if s, ok := a.(string); ok {
					to = append(to, types.StringValue(s))
				}
			}
		}
		c.Mail = &mailModel{To: to}
	} else if !ok {
		c.Mail = nil
	}

	for name, target := range map[string]**hookModel{"slack": &c.Slack, "discord": &c.Discord, "teams": &c.Teams} {
		if m, ok := has(name); ok && (importing || *target != nil) {
			*target = &hookModel{WebhookURL: str(m, "webhook_url")}
		} else if !ok {
			*target = nil
		}
	}

	if m, ok := has("webhook"); ok && (importing || c.Webhook != nil) {
		c.Webhook = &webhookModel{URL: str(m, "url")}
	} else if !ok {
		c.Webhook = nil
	}

	if m, ok := has("github"); ok && (importing || c.Github != nil) {
		token := types.StringNull()
		if c.Github != nil {
			token = c.Github.Token
		}
		c.Github = &githubModel{Repo: str(m, "repo"), Token: token}
	} else if !ok {
		c.Github = nil
	}

	if m, ok := has("telegram"); ok && (importing || c.Telegram != nil) {
		token := types.StringNull()
		if c.Telegram != nil {
			token = c.Telegram.BotToken
		}
		c.Telegram = &telegramModel{ChatID: str(m, "chat_id"), BotToken: token}
	} else if !ok {
		c.Telegram = nil
	}

	if _, ok := has("pagerduty"); ok && (importing || c.Pagerduty != nil) {
		key := types.StringNull()
		if c.Pagerduty != nil {
			key = c.Pagerduty.RoutingKey
		}
		c.Pagerduty = &pagerdutyModel{RoutingKey: key}
	} else if !ok {
		c.Pagerduty = nil
	}

	if m, ok := has("opsgenie"); ok && (importing || c.Opsgenie != nil) {
		key := types.StringNull()
		if c.Opsgenie != nil {
			key = c.Opsgenie.APIKey
		}
		c.Opsgenie = &opsgenieModel{APIKey: key, Region: str(m, "region")}
	} else if !ok {
		c.Opsgenie = nil
	}
}
