package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tailcove/terraform-provider-tailcove/internal/client"
)

var (
	_ resource.Resource                = (*sourceResource)(nil)
	_ resource.ResourceWithConfigure   = (*sourceResource)(nil)
	_ resource.ResourceWithImportState = (*sourceResource)(nil)
)

func NewSourceResource() resource.Resource { return &sourceResource{} }

type sourceResource struct{ client *client.Client }

type sourceModel struct {
	ID            types.Int64  `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Platform      types.String `tfsdk:"platform"`
	RetentionDays types.Int64  `tfsdk:"retention_days"`
	ClientGroupID types.Int64  `tfsdk:"client_group_id"`
	IngestToken   types.String `tfsdk:"ingest_token"`
}

func (r *sourceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_source"
}

func (r *sourceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A log source. The token needs `sources:write`. The plan's source limit applies.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{Required: true, Description: "The source's name."},
			"platform": schema.StringAttribute{
				Optional:    true,
				Description: "What sends the logs, for example `laravel` or `nginx`.",
			},
			"retention_days": schema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Description:   "Days the logs are kept; at most the plan's. Defaults to the plan's retention.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"client_group_id": schema.Int64Attribute{
				Optional:    true,
				Description: "The client group the source belongs to.",
			},
			"ingest_token": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "The ingest token. Tailcove returns it once, when the source is created, so it is only known for a source this provider created; it is kept in the state, which therefore holds a secret. A source that was imported has none.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *sourceResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*client.Client); ok {
		r.client = c
	}
}

func (m sourceModel) body() map[string]any {
	return map[string]any{
		"name":            m.Name.ValueString(),
		"platform":        optStr(m.Platform),
		"retention_days":  optInt(m.RetentionDays),
		"client_group_id": optInt(m.ClientGroupID),
	}
}

// apply puts what the API says into the model.
func (m *sourceModel) apply(s *client.Source, importing bool) {
	m.ID = types.Int64Value(s.ID)
	m.Name = types.StringValue(s.Name)
	m.Platform = refStr(m.Platform, s.Platform, importing)
	m.RetentionDays = types.Int64Value(s.RetentionDays)
	m.ClientGroupID = refInt(m.ClientGroupID, s.ClientGroupID, importing)
	if s.IngestToken != "" {
		m.IngestToken = types.StringValue(s.IngestToken)
	} else if m.IngestToken.IsUnknown() {
		m.IngestToken = types.StringNull()
	}
}

func (r *sourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	s, err := r.client.CreateSource(ctx, plan.body())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the source", err)
		return
	}
	plan.apply(s, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	s, err := r.client.GetSource(ctx, state.ID.ValueInt64())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not read the source", err)
		return
	}
	state.apply(s, state.Name.IsNull())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *sourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	s, err := r.client.UpdateSource(ctx, plan.ID.ValueInt64(), plan.body())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the source", err)
		return
	}
	plan.apply(s, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Deleting a source deletes its logs and alerts: only owners and admins may, and so may their tokens.
	if err := r.client.DeleteSource(ctx, state.ID.ValueInt64()); err != nil && !errors.Is(err, client.ErrNotFound) {
		addAPIError(&resp.Diagnostics, "Could not delete the source", err)
	}
}

func (r *sourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
