// Package provider is the Terraform provider for Tailcove.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tailcove/terraform-provider-tailcove/internal/client"
)

var _ provider.Provider = (*tailcoveProvider)(nil)

type tailcoveProvider struct {
	version string
}

// New returns the provider factory.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &tailcoveProvider{version: version} }
}

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Token    types.String `tfsdk:"token"`
}

func (p *tailcoveProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "tailcove"
	resp.Version = p.version
}

func (p *tailcoveProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage sources, uptime monitors and alerts of a Tailcove team. A token acts for the team it was made for, so one provider configuration manages one team.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "The Tailcove app's address. Defaults to " + client.DefaultEndpoint + " (or the TAILCOVE_ENDPOINT environment variable).",
			},
			"token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "An API token with the abilities of the resources you manage (sources:write, monitors:write, alerts:write, oncall:read). Defaults to the TAILCOVE_TOKEN environment variable.",
			},
		},
	}
}

func (p *tailcoveProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if cfg.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Unknown token", "The token is not known yet; set it from a value that is known at plan time, or use TAILCOVE_TOKEN.")
		return
	}

	endpoint := os.Getenv("TAILCOVE_ENDPOINT")
	if !cfg.Endpoint.IsNull() && !cfg.Endpoint.IsUnknown() {
		endpoint = cfg.Endpoint.ValueString()
	}
	token := os.Getenv("TAILCOVE_TOKEN")
	if !cfg.Token.IsNull() {
		token = cfg.Token.ValueString()
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing token", "Set the token in the provider block or the TAILCOVE_TOKEN environment variable.")
		return
	}

	c := client.New(endpoint, token, "terraform-provider-tailcove/"+p.version)
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *tailcoveProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewSourceResource, NewMonitorResource, NewAlertResource}
}

func (p *tailcoveProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{NewSourceDataSource, NewEscalationPolicyDataSource, NewScheduleDataSource}
}
