package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tailcove/terraform-provider-tailcove/internal/client"
)

var (
	_ resource.Resource                = (*alertResource)(nil)
	_ resource.ResourceWithConfigure   = (*alertResource)(nil)
	_ resource.ResourceWithImportState = (*alertResource)(nil)
)

func NewAlertResource() resource.Resource { return &alertResource{} }

type alertResource struct{ client *client.Client }

type alertModel struct {
	ID                 types.Int64    `tfsdk:"id"`
	SourceID           types.Int64    `tfsdk:"source_id"`
	Name               types.String   `tfsdk:"name"`
	Type               types.String   `tfsdk:"type"`
	Query              types.String   `tfsdk:"query"`
	ThresholdCount     types.Int64    `tfsdk:"threshold_count"`
	NoData             types.Bool     `tfsdk:"no_data"`
	AnomalyMultiple    types.Float64  `tfsdk:"anomaly_multiple"`
	MetricName         types.String   `tfsdk:"metric_name"`
	MetricHost         types.String   `tfsdk:"metric_host"`
	MetricAggregate    types.String   `tfsdk:"metric_aggregate"`
	MetricOperator     types.String   `tfsdk:"metric_operator"`
	MetricThreshold    types.Float64  `tfsdk:"metric_threshold"`
	WindowMinutes      types.Int64    `tfsdk:"window_minutes"`
	CooldownMinutes    types.Int64    `tfsdk:"cooldown_minutes"`
	IsActive           types.Bool     `tfsdk:"is_active"`
	CreateIncident     types.Bool     `tfsdk:"create_incident"`
	EscalationPolicyID types.Int64    `tfsdk:"escalation_policy_id"`
	Channels           *channelsModel `tfsdk:"channels"`
	CreatedAt          types.String   `tfsdk:"created_at"`
}

func (r *alertResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert"
}

func (r *alertResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	optStrA := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, Description: desc}
	}
	optIntA := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{Optional: true, Description: desc}
	}
	optFloatA := func(desc string) schema.Float64Attribute {
		return schema.Float64Attribute{Optional: true, Description: desc}
	}

	resp.Schema = schema.Schema{
		Description: "An alert on a source. The token needs `alerts:write`. A `log` alert counts the lines a query matches in a window; " +
			"a `metric` alert judges a metric; an `anomaly` alert compares with the usual count. Import with `source_id/alert_id`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{Computed: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}},
			"source_id": schema.Int64Attribute{
				Required:      true,
				Description:   "The source the alert belongs to. Changing it replaces the alert.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{Required: true, Description: "The alert's name."},
			"type": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("log"),
				Description: "`log` (the default), `metric` or `anomaly`.",
			},
			"query":           optStrA("log and anomaly: the search query whose matches are counted."),
			"threshold_count": optIntA("log and anomaly: fire at this many matches in the window (anomaly: the least count worth an alert)."),
			"no_data": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Fire on silence instead of on a count.",
			},
			"anomaly_multiple": optFloatA("anomaly: fire at this multiple of the usual count, 1.5 to 100."),
			"metric_name":      optStrA("metric: the metric."),
			"metric_host":      optStrA("metric: limit to one host."),
			"metric_aggregate": optStrA("metric: how to combine the points, for example `avg`, `max`, `min`, `sum`."),
			"metric_operator":  optStrA("metric: `gt` or `lt`."),
			"metric_threshold": optFloatA("metric: the value the aggregate is compared with."),
			"window_minutes":   schema.Int64Attribute{Required: true, Description: "The window, 1 to 1440 minutes."},
			"cooldown_minutes": schema.Int64Attribute{Required: true, Description: "Minutes before it may fire again, 1 to 1440."},
			"is_active": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether the alert is evaluated.",
			},
			"create_incident": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Open an incident when the alert fires.",
			},
			"escalation_policy_id": optIntA("The escalation policy an incident follows (see the `tailcove_escalation_policy` data source)."),
			"channels":             channelsAttribute(),
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *alertResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*client.Client); ok {
		r.client = c
	}
}

func (m alertModel) body() map[string]any {
	return map[string]any{
		"name":                 m.Name.ValueString(),
		"type":                 optStr(m.Type),
		"query":                optStr(m.Query),
		"threshold_count":      optInt(m.ThresholdCount),
		"no_data":              optBool(m.NoData),
		"anomaly_multiple":     optFloat(m.AnomalyMultiple),
		"metric_name":          optStr(m.MetricName),
		"metric_host":          optStr(m.MetricHost),
		"metric_aggregate":     optStr(m.MetricAggregate),
		"metric_operator":      optStr(m.MetricOperator),
		"metric_threshold":     optFloat(m.MetricThreshold),
		"window_minutes":       optInt(m.WindowMinutes),
		"cooldown_minutes":     optInt(m.CooldownMinutes),
		"is_active":            optBool(m.IsActive),
		"create_incident":      optBool(m.CreateIncident),
		"escalation_policy_id": optInt(m.EscalationPolicyID),
		"channels":             m.Channels.toAPI(),
	}
}

func (m *alertModel) apply(a *client.Alert, importing bool) {
	m.ID = types.Int64Value(a.ID)
	m.SourceID = types.Int64Value(a.SourceID)
	m.Name = types.StringValue(a.Name)
	m.Type = types.StringValue(a.Type)
	m.Query = refStr(m.Query, a.Query, importing)
	// A metric alert is stored with a count of 1 whatever was sent, and so is a no-data alert: keep what was written.
	m.ThresholdCount = refInt(m.ThresholdCount, a.ThresholdCount, importing)
	m.NoData = types.BoolValue(a.NoData)
	m.AnomalyMultiple = refFloat(m.AnomalyMultiple, a.AnomalyMultiple, importing)
	m.MetricName = refStr(m.MetricName, a.MetricName, importing)
	m.MetricHost = refStr(m.MetricHost, a.MetricHost, importing)
	m.MetricAggregate = refStr(m.MetricAggregate, a.MetricAggregate, importing)
	m.MetricOperator = refStr(m.MetricOperator, a.MetricOperator, importing)
	m.MetricThreshold = refFloat(m.MetricThreshold, a.MetricThreshold, importing)
	m.WindowMinutes = types.Int64Value(a.WindowMinutes)
	m.CooldownMinutes = types.Int64Value(a.CooldownMinutes)
	m.IsActive = types.BoolValue(a.IsActive)
	m.CreateIncident = types.BoolValue(a.CreateIncident)
	m.EscalationPolicyID = refInt(m.EscalationPolicyID, a.EscalationPolicyID, importing)

	if m.Channels == nil {
		m.Channels = &channelsModel{}
	}
	m.Channels.refresh(a.Channels, importing)

	m.CreatedAt = strOrNull(a.CreatedAt)
}

func (r *alertResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan alertModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	a, err := r.client.CreateAlert(ctx, plan.SourceID.ValueInt64(), plan.body())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the alert", err)
		return
	}
	plan.apply(a, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *alertResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state alertModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	a, err := r.client.GetAlert(ctx, state.SourceID.ValueInt64(), state.ID.ValueInt64())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the alert", err)
		return
	}
	state.apply(a, state.Name.IsNull())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *alertResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan alertModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	a, err := r.client.UpdateAlert(ctx, plan.SourceID.ValueInt64(), plan.ID.ValueInt64(), plan.body())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the alert", err)
		return
	}
	plan.apply(a, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *alertResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state alertModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteAlert(ctx, state.SourceID.ValueInt64(), state.ID.ValueInt64()); err != nil && !errors.Is(err, client.ErrNotFound) {
		addAPIError(&resp.Diagnostics, "Could not delete the alert", err)
	}
}

// ImportState takes `source_id/alert_id`: an alert is addressed through its source.
func (r *alertResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	sourcePart, alertPart, ok := strings.Cut(req.ID, "/")
	if !ok {
		resp.Diagnostics.AddError("Invalid import id", fmt.Sprintf("Use source_id/alert_id, for example 12/34; got %q.", req.ID))
		return
	}
	sourceID, err1 := parseID(sourcePart)
	alertID, err2 := parseID(alertPart)
	if err := errors.Join(err1, err2); err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("source_id"), sourceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), alertID)...)
}
