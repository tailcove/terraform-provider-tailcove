package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tailcove/terraform-provider-tailcove/internal/client"
)

var (
	_ datasource.DataSourceWithConfigure = (*sourceDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*escalationPolicyDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*scheduleDataSource)(nil)
)

// pick finds the one item whose name matches, and says what was wrong when there is none or several.
func pick[T any](kind, name string, items []T, nameOf func(T) string) (T, error) {
	var found []T
	for _, item := range items {
		if nameOf(item) == name {
			found = append(found, item)
		}
	}
	var zero T
	switch len(found) {
	case 0:
		return zero, fmt.Errorf("no %s is named %q (a token only sees its own team's)", kind, name)
	case 1:
		return found[0], nil
	default:
		return zero, fmt.Errorf("%d %ss are named %q; names are not unique, so use the id", len(found), kind, name)
	}
}

// --- source ---

func NewSourceDataSource() datasource.DataSource { return &sourceDataSource{} }

type sourceDataSource struct{ client *client.Client }

type sourceDataModel struct {
	ID            types.Int64  `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Platform      types.String `tfsdk:"platform"`
	RetentionDays types.Int64  `tfsdk:"retention_days"`
	ClientGroupID types.Int64  `tfsdk:"client_group_id"`
}

func (d *sourceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_source"
}

func (d *sourceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A source that already exists, by id or by name. The token needs `sources:read`.",
		Attributes: map[string]schema.Attribute{
			"id":              schema.Int64Attribute{Optional: true, Computed: true, Description: "The source's id. Set this or `name`."},
			"name":            schema.StringAttribute{Optional: true, Computed: true, Description: "The source's name. Set this or `id`."},
			"platform":        schema.StringAttribute{Computed: true},
			"retention_days":  schema.Int64Attribute{Computed: true},
			"client_group_id": schema.Int64Attribute{Computed: true},
		},
	}
}

func (d *sourceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*client.Client); ok {
		d.client = c
	}
}

func (d *sourceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg sourceDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.ID.IsNull() == cfg.Name.IsNull() {
		resp.Diagnostics.AddError("Set id or name", "Set exactly one of id and name.")
		return
	}

	var s *client.Source
	if !cfg.ID.IsNull() {
		got, err := d.client.GetSource(ctx, cfg.ID.ValueInt64())
		if err != nil {
			addAPIError(&resp.Diagnostics, "Could not read the source", err)
			return
		}
		s = got
	} else {
		all, err := d.client.ListSources(ctx)
		if err != nil {
			addAPIError(&resp.Diagnostics, "Could not list the sources", err)
			return
		}
		got, err := pick("source", cfg.Name.ValueString(), all, func(s client.Source) string { return s.Name })
		if err != nil {
			resp.Diagnostics.AddError("Source not found", err.Error())
			return
		}
		s = &got
	}

	out := sourceDataModel{
		ID:            types.Int64Value(s.ID),
		Name:          types.StringValue(s.Name),
		Platform:      strOrNull(s.Platform),
		RetentionDays: types.Int64Value(s.RetentionDays),
		ClientGroupID: intOrNull(s.ClientGroupID),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}

// --- escalation policy and schedule: the API lists them; the web app edits them ---

type namedDataModel struct {
	ID   types.Int64  `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func nameSchema(desc string) schema.Schema {
	return schema.Schema{
		Description: desc,
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{Required: true, Description: "The exact name."},
			"id":   schema.Int64Attribute{Computed: true},
		},
	}
}

func NewEscalationPolicyDataSource() datasource.DataSource { return &escalationPolicyDataSource{} }

type escalationPolicyDataSource struct{ client *client.Client }

func (d *escalationPolicyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_escalation_policy"
}

func (d *escalationPolicyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = nameSchema("An escalation policy, by name, for the `escalation_policy_id` of a monitor or an alert. Policies are edited in the Tailcove app. The token needs `oncall:read`.")
}

func (d *escalationPolicyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*client.Client); ok {
		d.client = c
	}
}

func (d *escalationPolicyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg namedDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := d.client.ListEscalationPolicies(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not list the escalation policies", err)
		return
	}
	p, err := pick("escalation policy", cfg.Name.ValueString(), all, func(p client.EscalationPolicy) string { return p.Name })
	if err != nil {
		resp.Diagnostics.AddError("Escalation policy not found", err.Error())
		return
	}
	cfg.ID = types.Int64Value(p.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

func NewScheduleDataSource() datasource.DataSource { return &scheduleDataSource{} }

type scheduleDataSource struct{ client *client.Client }

type scheduleDataModel struct {
	ID       types.Int64  `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	TimeZone types.String `tfsdk:"time_zone"`
}

func (d *scheduleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_oncall_schedule"
}

func (d *scheduleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	s := nameSchema("An on-call schedule, by name. Schedules are edited in the Tailcove app. The token needs `oncall:read`.")
	s.Attributes["time_zone"] = schema.StringAttribute{Computed: true}
	resp.Schema = s
}

func (d *scheduleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*client.Client); ok {
		d.client = c
	}
}

func (d *scheduleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg scheduleDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := d.client.ListSchedules(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not list the on-call schedules", err)
		return
	}
	s, err := pick("on-call schedule", cfg.Name.ValueString(), all, func(s client.Schedule) string { return s.Name })
	if err != nil {
		resp.Diagnostics.AddError("Schedule not found", err.Error())
		return
	}
	cfg.ID = types.Int64Value(s.ID)
	cfg.TimeZone = types.StringValue(s.TimeZone)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
