package provider

import (
	"context"
	"errors"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	_ resource.Resource                = (*monitorResource)(nil)
	_ resource.ResourceWithConfigure   = (*monitorResource)(nil)
	_ resource.ResourceWithImportState = (*monitorResource)(nil)
)

func NewMonitorResource() resource.Resource { return &monitorResource{} }

type monitorResource struct{ client *client.Client }

type monitorModel struct {
	ID                 types.Int64    `tfsdk:"id"`
	Name               types.String   `tfsdk:"name"`
	Type               types.String   `tfsdk:"type"`
	URL                types.String   `tfsdk:"url"`
	Method             types.String   `tfsdk:"method"`
	ExpectedStatus     types.Int64    `tfsdk:"expected_status"`
	Keyword            types.String   `tfsdk:"keyword"`
	Host               types.String   `tfsdk:"host"`
	Port               types.Int64    `tfsdk:"port"`
	DNSRecordType      types.String   `tfsdk:"dns_record_type"`
	DNSExpected        types.String   `tfsdk:"dns_expected"`
	WarnDays           types.Int64    `tfsdk:"warn_days"`
	IntervalSeconds    types.Int64    `tfsdk:"interval_seconds"`
	TimeoutMs          types.Int64    `tfsdk:"timeout_ms"`
	GraceSeconds       types.Int64    `tfsdk:"grace_seconds"`
	RequestHeaders     types.Map      `tfsdk:"request_headers"`
	RequestBody        types.String   `tfsdk:"request_body"`
	Regions            types.Set      `tfsdk:"regions"`
	MinRegionsDown     types.Int64    `tfsdk:"min_regions_down"`
	IsActive           types.Bool     `tfsdk:"is_active"`
	CreateIncident     types.Bool     `tfsdk:"create_incident"`
	EscalationPolicyID types.Int64    `tfsdk:"escalation_policy_id"`
	ClientGroupID      types.Int64    `tfsdk:"client_group_id"`
	Channels           *channelsModel `tfsdk:"channels"`
	State              types.String   `tfsdk:"state"`
	PingURL            types.String   `tfsdk:"ping_url"`
	CreatedAt          types.String   `tfsdk:"created_at"`
}

func (r *monitorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

func (r *monitorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	optStrA := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, Description: desc}
	}
	optIntA := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{Optional: true, Description: desc}
	}

	resp.Schema = schema.Schema{
		Description: "An uptime monitor. The token needs `monitors:write`. Which attributes a monitor needs depends on its `type`; " +
			"Tailcove checks them when the monitor is saved and the message names the attribute. The plan limits the number of monitors, the shortest interval and the number of regions.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.Int64Attribute{Computed: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Description: "The monitor's name."},
			"type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("http"),
				Description: "`http` (the default), `tcp`, `icmp`, `heartbeat`, `dns`, `ssl` or `domain`.",
			},
			"url":              optStrA("http: the address to request (http or https)."),
			"method":           optStrA("http: the request method, for example `GET`."),
			"expected_status":  optIntA("http: the status code that means up."),
			"keyword":          optStrA("http: text the response must contain."),
			"host":             optStrA("tcp, icmp, dns, ssl, domain: the host (domain monitors: a registered domain)."),
			"port":             optIntA("tcp, ssl: the port."),
			"dns_record_type":  optStrA("dns: A, AAAA, CNAME, TXT, MX or NS."),
			"dns_expected":     optStrA("dns: the value the record must have."),
			"warn_days":        optIntA("ssl, domain: warn this many days before the certificate or domain expires."),
			"interval_seconds": optIntA("Seconds between checks (heartbeat: how often the job is expected to ping). The plan sets the shortest."),
			"timeout_ms":       optIntA("http, tcp, icmp, ssl: milliseconds to wait, 1000 to 30000."),
			"grace_seconds":    optIntA("heartbeat: how much longer than the interval to wait before calling it down."),
			"request_headers": schema.MapAttribute{
				Optional:    true,
				Sensitive:   true,
				ElementType: types.StringType,
				Description: "http: headers to send, name to value. Values are stored encrypted and never returned, so a change made outside Terraform is not detected for them.",
			},
			"request_body": optStrA("http: the body, for a method that has one."),
			"regions": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Probe regions. The plan limits how many.",
			},
			"min_regions_down": optIntA("How many regions must see it down before it is down, 1 to 3."),
			"is_active": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether the monitor runs.",
			},
			"create_incident": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Open an incident when the monitor goes down.",
			},
			"escalation_policy_id": optIntA("The escalation policy an incident follows (see the `tailcove_escalation_policy` data source)."),
			"client_group_id":      optIntA("The client group the monitor belongs to."),
			"channels":             channelsAttribute(),
			"state":                schema.StringAttribute{Computed: true, Description: "`up`, `down` or `unknown`."},
			"ping_url": schema.StringAttribute{
				Computed:    true,
				Description: "heartbeat: the address the job pings.",
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *monitorResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*client.Client); ok {
		r.client = c
	}
}

func (m monitorModel) body() map[string]any {
	body := map[string]any{
		"name":                 m.Name.ValueString(),
		"type":                 optStr(m.Type),
		"url":                  optStr(m.URL),
		"method":               optStr(m.Method),
		"expected_status":      optInt(m.ExpectedStatus),
		"keyword":              optStr(m.Keyword),
		"host":                 optStr(m.Host),
		"port":                 optInt(m.Port),
		"dns_record_type":      optStr(m.DNSRecordType),
		"dns_expected":         optStr(m.DNSExpected),
		"warn_days":            optInt(m.WarnDays),
		"interval_seconds":     optInt(m.IntervalSeconds),
		"timeout_ms":           optInt(m.TimeoutMs),
		"grace_seconds":        optInt(m.GraceSeconds),
		"request_body":         optStr(m.RequestBody),
		"min_regions_down":     optInt(m.MinRegionsDown),
		"is_active":            optBool(m.IsActive),
		"create_incident":      optBool(m.CreateIncident),
		"escalation_policy_id": optInt(m.EscalationPolicyID),
		"client_group_id":      optInt(m.ClientGroupID),
		"channels":             m.Channels.toAPI(),
		"regions":              nil,
		"request_headers":      nil,
	}

	if !m.Regions.IsNull() && !m.Regions.IsUnknown() {
		regions := make([]string, 0, len(m.Regions.Elements()))
		for _, e := range m.Regions.Elements() {
			if s, ok := e.(types.String); ok {
				regions = append(regions, s.ValueString())
			}
		}
		sort.Strings(regions)
		body["regions"] = regions
	}

	if !m.RequestHeaders.IsNull() && !m.RequestHeaders.IsUnknown() {
		names := make([]string, 0, len(m.RequestHeaders.Elements()))
		values := map[string]string{}
		for name, e := range m.RequestHeaders.Elements() {
			if s, ok := e.(types.String); ok {
				names = append(names, name)
				values[name] = s.ValueString()
			}
		}
		sort.Strings(names)
		headers := make([]map[string]string, 0, len(names))
		for _, name := range names {
			headers = append(headers, map[string]string{"name": name, "value": values[name]})
		}
		body["request_headers"] = headers
	}

	return body
}

// apply puts what the API says into the model; see refStr for why most attributes only follow when set.
func (m *monitorModel) apply(a *client.Monitor, importing bool) {
	m.ID = types.Int64Value(a.ID)
	m.Name = types.StringValue(a.Name)
	m.Type = types.StringValue(a.Type)
	m.URL = refStr(m.URL, a.URL, importing)
	m.Method = refStr(m.Method, a.Method, importing)
	m.ExpectedStatus = refInt(m.ExpectedStatus, a.ExpectedStatus, importing)
	m.Keyword = refStr(m.Keyword, a.Keyword, importing)
	m.Host = refStr(m.Host, a.Host, importing)
	m.Port = refInt(m.Port, a.Port, importing)
	m.DNSRecordType = refStr(m.DNSRecordType, a.DNSRecordType, importing)
	m.DNSExpected = refStr(m.DNSExpected, a.DNSExpected, importing)
	m.WarnDays = refInt(m.WarnDays, a.WarnDays, importing)
	m.IntervalSeconds = refInt(m.IntervalSeconds, a.IntervalSeconds, importing)
	m.TimeoutMs = refInt(m.TimeoutMs, a.TimeoutMs, importing)
	m.GraceSeconds = refInt(m.GraceSeconds, a.GraceSeconds, importing)
	m.RequestBody = refStr(m.RequestBody, a.RequestBody, importing)
	m.MinRegionsDown = refInt(m.MinRegionsDown, a.MinRegionsDown, importing)
	m.EscalationPolicyID = refInt(m.EscalationPolicyID, a.EscalationPolicyID, importing)
	m.ClientGroupID = refInt(m.ClientGroupID, a.ClientGroupID, importing)
	m.IsActive = types.BoolValue(a.IsActive)
	m.CreateIncident = types.BoolValue(a.CreateIncident)

	if importing || !m.Regions.IsNull() {
		if len(a.Regions) == 0 {
			m.Regions = types.SetNull(types.StringType)
		} else {
			elems := make([]attr.Value, 0, len(a.Regions))
			for _, r := range a.Regions {
				elems = append(elems, types.StringValue(r))
			}
			m.Regions = types.SetValueMust(types.StringType, elems)
		}
	}

	if m.RequestHeaders.IsUnknown() {
		m.RequestHeaders = types.MapNull(types.StringType)
	}

	if m.Channels == nil {
		m.Channels = &channelsModel{}
	}
	m.Channels.refresh(a.Channels, importing)

	m.State = types.StringValue(a.State)
	m.PingURL = strOrNull(a.PingURL)
	m.CreatedAt = strOrNull(a.CreatedAt)
}

func (r *monitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan monitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	a, err := r.client.CreateMonitor(ctx, plan.body())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the monitor", err)
		return
	}
	plan.apply(a, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *monitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state monitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	a, err := r.client.GetMonitor(ctx, state.ID.ValueInt64())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the monitor", err)
		return
	}
	state.apply(a, state.Name.IsNull())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *monitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan monitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	a, err := r.client.UpdateMonitor(ctx, plan.ID.ValueInt64(), plan.body())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the monitor", err)
		return
	}
	plan.apply(a, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *monitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state monitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteMonitor(ctx, state.ID.ValueInt64()); err != nil && !errors.Is(err, client.ErrNotFound) {
		addAPIError(&resp.Diagnostics, "Could not delete the monitor", err)
	}
}

func (r *monitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
