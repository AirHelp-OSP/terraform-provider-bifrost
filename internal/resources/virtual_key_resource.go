package resources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	bifrostclient "github.com/airhelp-osp/terraform-provider-bifrost/internal/client"
)

var _ resource.Resource = &VirtualKeyResource{}
var _ resource.ResourceWithImportState = &VirtualKeyResource{}
var _ resource.ResourceWithConfigValidators = &VirtualKeyResource{}

// NewVirtualKeyResource returns a new VirtualKeyResource.
func NewVirtualKeyResource() resource.Resource {
	return &VirtualKeyResource{}
}

// VirtualKeyResource manages bifrost_virtual_key resources.
type VirtualKeyResource struct {
	client *bifrostclient.BifrostClient
}

// ── Model types ───────────────────────────────────────────────────────────────

type VirtualKeyResourceModel struct {
	ID              types.String            `tfsdk:"id"`
	Name            types.String            `tfsdk:"name"`
	Description     types.String            `tfsdk:"description"`
	Value           types.String            `tfsdk:"value"`
	IsActive        types.Bool              `tfsdk:"is_active"`
	TeamID          types.String            `tfsdk:"team_id"`
	CustomerID      types.String            `tfsdk:"customer_id"`
	ProviderConfigs []VKProviderConfigModel `tfsdk:"provider_configs"`
	Budget          *VKBudgetModel          `tfsdk:"budget"`
	RateLimit       *VKRateLimitModel       `tfsdk:"rate_limit"`
}

type VKProviderConfigModel struct {
	Provider      types.String      `tfsdk:"provider"`
	Weight        types.Float64     `tfsdk:"weight"`
	AllowedModels []types.String    `tfsdk:"allowed_models"`
	KeyIDs        []types.String    `tfsdk:"key_ids"`
	Budget        *VKBudgetModel    `tfsdk:"budget"`
	RateLimit     *VKRateLimitModel `tfsdk:"rate_limit"`
}

type VKBudgetModel struct {
	MaxLimit        types.Float64 `tfsdk:"max_limit"`
	ResetDuration   types.String  `tfsdk:"reset_duration"`
	CalendarAligned types.Bool    `tfsdk:"calendar_aligned"`
}

type VKRateLimitModel struct {
	TokenMaxLimit        types.Int64  `tfsdk:"token_max_limit"`
	TokenResetDuration   types.String `tfsdk:"token_reset_duration"`
	RequestMaxLimit      types.Int64  `tfsdk:"request_max_limit"`
	RequestResetDuration types.String `tfsdk:"request_reset_duration"`
}

// ── Schema ────────────────────────────────────────────────────────────────────

func (r *VirtualKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_key"
}

var budgetSchema = schema.SingleNestedAttribute{
	MarkdownDescription: "Budget configuration. `reset_duration` accepts Bifrost duration strings such as `1d`, `1w`, or `1M`.",
	Description:         "Budget configuration.",
	Optional:            true,
	Attributes: map[string]schema.Attribute{
		"max_limit": schema.Float64Attribute{
			MarkdownDescription: "Maximum budget in dollars.",
			Description:         "Maximum budget in dollars.",
			Required:            true,
		},
		"reset_duration": schema.StringAttribute{
			MarkdownDescription: "Budget reset period (e.g. `1d`, `1w`, `1M`).",
			Description:         "Budget reset period (e.g. '1d', '1w', '1M').",
			Required:            true,
		},
		"calendar_aligned": schema.BoolAttribute{
			MarkdownDescription: "Snap resets to calendar boundaries (start of day/week/month) instead of rolling. Defaults to `false`.",
			Description:         "Snap resets to calendar boundaries.",
			Optional:            true,
			Computed:            true,
			Default:             booldefault.StaticBool(false),
		},
	},
}

var rateLimitSchema = schema.SingleNestedAttribute{
	MarkdownDescription: "Rate-limit configuration. Reset durations accept Bifrost duration strings such as `1m` or `1h`.",
	Description:         "Rate limit configuration.",
	Optional:            true,
	Attributes: map[string]schema.Attribute{
		"token_max_limit": schema.Int64Attribute{
			MarkdownDescription: "Maximum token count per window.",
			Description:         "Maximum token count per window.",
			Optional:            true,
		},
		"token_reset_duration": schema.StringAttribute{
			MarkdownDescription: "Token-limit reset window (e.g. `1m`, `1h`).",
			Description:         "Token limit reset window (e.g. '1m', '1h').",
			Optional:            true,
		},
		"request_max_limit": schema.Int64Attribute{
			MarkdownDescription: "Maximum request count per window.",
			Description:         "Maximum request count per window.",
			Optional:            true,
		},
		"request_reset_duration": schema.StringAttribute{
			MarkdownDescription: "Request-limit reset window (e.g. `1m`, `1h`).",
			Description:         "Request limit reset window (e.g. '1m', '1h').",
			Optional:            true,
		},
	},
}

func (r *VirtualKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a [Bifrost virtual key](https://github.com/maximhq/bifrost) — a governance " +
			"token that scopes upstream provider access by allowed providers, models, budgets, and rate limits.",
		Description: "Manages a Bifrost virtual key (governance token).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "UUID assigned by Bifrost at creation time.",
				Description:         "UUID assigned by Bifrost.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name for the virtual key.",
				Description:         "Human-readable name for the virtual key.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Optional description.",
				Description:         "Optional description.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "The `sk-bf-...` token. Set only on create; preserved from state on subsequent reads " +
					"(Bifrost does not return it after creation).",
				Description: "The sk-bf-... token. Set only on create; preserved from state on subsequent reads.",
				Computed:    true,
				Sensitive:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"is_active": schema.BoolAttribute{
				MarkdownDescription: "Whether the virtual key is active. Defaults to `true`.",
				Description:         "Whether the virtual key is active.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"team_id": schema.StringAttribute{
				MarkdownDescription: "Team UUID to associate with this key. **Mutually exclusive** with `customer_id`. " +
					"_Note: teams are an enterprise feature._",
				Description: "Team UUID to associate with this key (mutually exclusive with customer_id).",
				Optional:    true,
			},
			"customer_id": schema.StringAttribute{
				MarkdownDescription: "Customer UUID to associate with this key. **Mutually exclusive** with `team_id`. " +
					"_Note: customers are an enterprise feature._",
				Description: "Customer UUID to associate with this key (mutually exclusive with team_id).",
				Optional:    true,
			},
			"provider_configs": schema.ListNestedAttribute{
				MarkdownDescription: "Per-provider configuration restrictions. Each entry binds an upstream provider, " +
					"its allowed models, optional key IDs, and an optional budget/rate-limit override.",
				Description: "Per-provider configuration restrictions for this virtual key.",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"provider": schema.StringAttribute{
							MarkdownDescription: "Provider name (e.g. `bedrock`, `openai`).",
							Description:         "Provider name (e.g. 'bedrock', 'openai').",
							Required:            true,
						},
						"weight": schema.Float64Attribute{
							MarkdownDescription: "Routing weight for this provider (relative to other entries).",
							Description:         "Routing weight for this provider.",
							Optional:            true,
						},
						"allowed_models": schema.ListAttribute{
							MarkdownDescription: "Models permitted for this provider. Use `[\"*\"]` to allow all. " +
								"**Bifrost v1.5.0 changed the empty-list semantic**: `[]` now means _deny all_, " +
								"not _allow all_. Provider validates that `\"*\"` is not mixed with specific values.",
							Description: "Models permitted for this provider. ['*'] means all; [] means none (v1.5.0+).",
							Optional:    true,
							ElementType: types.StringType,
							Validators: []validator.List{
								listvalidator.UniqueValues(),
								WildcardNotMixed(),
							},
						},
						"key_ids": schema.ListAttribute{
							MarkdownDescription: "Specific key UUIDs to allow for this provider. " +
								"Use `[\"*\"]` for all keys, `[]` (or omit) to deny all (Bifrost v1.5.0 deny-by-default).",
							Description: "Specific key UUIDs to allow for this provider.",
							Optional:    true,
							ElementType: types.StringType,
							Validators: []validator.List{
								listvalidator.UniqueValues(),
								WildcardNotMixed(),
							},
						},
						"budget":     budgetSchema,
						"rate_limit": rateLimitSchema,
					},
				},
			},
			"budget":     budgetSchema,
			"rate_limit": rateLimitSchema,
		},
	}
}

// ── CRUD ──────────────────────────────────────────────────────────────────────

func (r *VirtualKeyResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.Conflicting(
			path.MatchRoot("team_id"),
			path.MatchRoot("customer_id"),
		),
	}
}

func (r *VirtualKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*bifrostclient.BifrostClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type",
			fmt.Sprintf("Expected *client.BifrostClient, got %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *VirtualKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VirtualKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "creating Bifrost virtual key", map[string]any{"name": plan.Name.ValueString()})

	createReq := vkModelToCreateRequest(plan)

	apiResp, err := r.client.CreateVirtualKey(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Error creating virtual key", err.Error())
		return
	}

	newState := vkResponseToModel(apiResp, &plan)

	tflog.Debug(ctx, "created Bifrost virtual key", map[string]any{"id": newState.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *VirtualKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state VirtualKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "reading Bifrost virtual key", map[string]any{"id": state.ID.ValueString()})

	apiResp, err := r.client.GetVirtualKey(ctx, state.ID.ValueString())
	if err != nil {
		if bifrostclient.IsNotFound(err) {
			tflog.Debug(ctx, "Bifrost virtual key not found, removing from state",
				map[string]any{"id": state.ID.ValueString()})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading virtual key", err.Error())
		return
	}

	newState := vkResponseToModel(apiResp, &state)

	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *VirtualKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan VirtualKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state VirtualKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "updating Bifrost virtual key", map[string]any{"id": state.ID.ValueString()})

	updateReq := vkModelToUpdateRequest(plan)

	apiResp, err := r.client.UpdateVirtualKey(ctx, state.ID.ValueString(), updateReq)
	if err != nil {
		resp.Diagnostics.AddError("Error updating virtual key", err.Error())
		return
	}

	newState := vkResponseToModel(apiResp, &plan)

	// Preserve the sk-bf-... value from state (not returned by update).
	if newState.Value.IsNull() || newState.Value.IsUnknown() || newState.Value.ValueString() == "" {
		newState.Value = state.Value
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *VirtualKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state VirtualKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "deleting Bifrost virtual key", map[string]any{"id": state.ID.ValueString()})

	err := r.client.DeleteVirtualKey(ctx, state.ID.ValueString())
	if err != nil && !bifrostclient.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting virtual key", err.Error())
		return
	}
}

func (r *VirtualKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func vkModelToCreateRequest(m VirtualKeyResourceModel) bifrostclient.CreateVirtualKeyRequest {
	req := bifrostclient.CreateVirtualKeyRequest{
		Name:        m.Name.ValueString(),
		Description: m.Description.ValueString(),
	}

	if !m.IsActive.IsNull() && !m.IsActive.IsUnknown() {
		v := m.IsActive.ValueBool()
		req.IsActive = &v
	}
	if !m.TeamID.IsNull() && !m.TeamID.IsUnknown() && m.TeamID.ValueString() != "" {
		v := m.TeamID.ValueString()
		req.TeamID = &v
	}
	if !m.CustomerID.IsNull() && !m.CustomerID.IsUnknown() && m.CustomerID.ValueString() != "" {
		v := m.CustomerID.ValueString()
		req.CustomerID = &v
	}
	if m.Budget != nil {
		req.Budget = modelToBudgetCreate(m.Budget)
	}
	if m.RateLimit != nil {
		req.RateLimit = modelToRateLimit(m.RateLimit)
	}
	for _, pc := range m.ProviderConfigs {
		req.ProviderConfigs = append(req.ProviderConfigs, modelToVKProviderConfigCreate(pc))
	}
	return req
}

func vkModelToUpdateRequest(m VirtualKeyResourceModel) bifrostclient.UpdateVirtualKeyRequest {
	name := m.Name.ValueString()
	desc := m.Description.ValueString()
	req := bifrostclient.UpdateVirtualKeyRequest{
		Name:        &name,
		Description: &desc,
	}

	if !m.IsActive.IsNull() && !m.IsActive.IsUnknown() {
		v := m.IsActive.ValueBool()
		req.IsActive = &v
	}

	// team_id and customer_id are always sent so a removed config attribute
	// becomes a JSON null on the wire, which Bifrost treats as an explicit
	// clear. Unknown values are skipped (treated like absent).
	if !m.TeamID.IsUnknown() && !m.TeamID.IsNull() && m.TeamID.ValueString() != "" {
		v := m.TeamID.ValueString()
		req.TeamID = &v
	}
	if !m.CustomerID.IsUnknown() && !m.CustomerID.IsNull() && m.CustomerID.ValueString() != "" {
		v := m.CustomerID.ValueString()
		req.CustomerID = &v
	}

	if m.Budget != nil {
		req.Budget = modelToBudgetUpdate(m.Budget)
	} else {
		// Empty update budget triggers removal of any existing budget.
		req.Budget = &bifrostclient.VKUpdateBudget{}
	}
	if m.RateLimit != nil {
		req.RateLimit = modelToRateLimit(m.RateLimit)
	} else {
		req.RateLimit = &bifrostclient.VKRateLimit{}
	}

	for _, pc := range m.ProviderConfigs {
		req.ProviderConfigs = append(req.ProviderConfigs, modelToVKProviderConfigCreate(pc))
	}
	return req
}

func modelToBudgetCreate(m *VKBudgetModel) *bifrostclient.VKBudget {
	if m == nil {
		return nil
	}
	return &bifrostclient.VKBudget{
		MaxLimit:        m.MaxLimit.ValueFloat64(),
		ResetDuration:   m.ResetDuration.ValueString(),
		CalendarAligned: m.CalendarAligned.ValueBool(),
	}
}

func modelToBudgetUpdate(m *VKBudgetModel) *bifrostclient.VKUpdateBudget {
	if m == nil {
		return nil
	}
	maxLimit := m.MaxLimit.ValueFloat64()
	resetDur := m.ResetDuration.ValueString()
	calAligned := m.CalendarAligned.ValueBool()
	return &bifrostclient.VKUpdateBudget{
		MaxLimit:        &maxLimit,
		ResetDuration:   &resetDur,
		CalendarAligned: &calAligned,
	}
}

func modelToRateLimit(m *VKRateLimitModel) *bifrostclient.VKRateLimit {
	if m == nil {
		return nil
	}
	rl := &bifrostclient.VKRateLimit{}
	if !m.TokenMaxLimit.IsNull() && !m.TokenMaxLimit.IsUnknown() {
		v := m.TokenMaxLimit.ValueInt64()
		rl.TokenMaxLimit = &v
	}
	if !m.TokenResetDuration.IsNull() && !m.TokenResetDuration.IsUnknown() {
		v := m.TokenResetDuration.ValueString()
		rl.TokenResetDuration = &v
	}
	if !m.RequestMaxLimit.IsNull() && !m.RequestMaxLimit.IsUnknown() {
		v := m.RequestMaxLimit.ValueInt64()
		rl.RequestMaxLimit = &v
	}
	if !m.RequestResetDuration.IsNull() && !m.RequestResetDuration.IsUnknown() {
		v := m.RequestResetDuration.ValueString()
		rl.RequestResetDuration = &v
	}
	return rl
}

func modelToVKProviderConfigCreate(pc VKProviderConfigModel) bifrostclient.VKProviderConfigCreate {
	c := bifrostclient.VKProviderConfigCreate{
		Provider: pc.Provider.ValueString(),
		Weight:   pc.Weight.ValueFloat64(),
	}
	for _, m := range pc.AllowedModels {
		c.AllowedModels = append(c.AllowedModels, m.ValueString())
	}
	for _, k := range pc.KeyIDs {
		c.KeyIDs = append(c.KeyIDs, k.ValueString())
	}
	if pc.Budget != nil {
		c.Budget = modelToBudgetCreate(pc.Budget)
	}
	if pc.RateLimit != nil {
		c.RateLimit = modelToRateLimit(pc.RateLimit)
	}
	return c
}

// vkResponseToModel maps a VirtualKeyResponse to TF state, preserving the sk-bf-... value
// from prior state (the API does not return it after create).
func vkResponseToModel(apiResp *bifrostclient.VirtualKeyResponse, prior *VirtualKeyResourceModel) *VirtualKeyResourceModel {
	m := &VirtualKeyResourceModel{
		ID:          types.StringValue(apiResp.ID),
		Name:        types.StringValue(apiResp.Name),
		Description: types.StringValue(apiResp.Description),
		IsActive:    types.BoolValue(apiResp.IsActive),
	}

	if apiResp.Value != "" {
		m.Value = types.StringValue(apiResp.Value)
	} else if prior != nil {
		m.Value = prior.Value
	} else {
		m.Value = types.StringNull()
	}

	if apiResp.TeamID != nil {
		m.TeamID = types.StringValue(*apiResp.TeamID)
	} else {
		m.TeamID = types.StringNull()
	}
	if apiResp.CustomerID != nil {
		m.CustomerID = types.StringValue(*apiResp.CustomerID)
	} else {
		m.CustomerID = types.StringNull()
	}

	var priorBudget *VKBudgetModel
	if prior != nil {
		priorBudget = prior.Budget
	}
	m.Budget = apiRespToBudgetModel(apiResp.Budget, priorBudget)
	m.RateLimit = apiRespToRateLimitModel(apiResp.RateLimit)

	priorPCByProvider := map[string]VKProviderConfigModel{}
	if prior != nil {
		for _, pc := range prior.ProviderConfigs {
			priorPCByProvider[pc.Provider.ValueString()] = pc
		}
	}
	for _, pc := range apiResp.ProviderConfigs {
		priorPC := priorPCByProvider[pc.Provider]
		m.ProviderConfigs = append(m.ProviderConfigs, apiRespToVKProviderConfigModel(pc, priorPC))
	}

	return m
}

// NOTE (Bifrost v1.5.0 BC6): the migration guide forward-recommends moving from
// the singular `budget` object to a `budgets` array on both Virtual Keys and
// their provider configs. The v1.5.0 OpenAPI still exposes singular `budget`
// on CreateVirtualKeyRequest/UpdateVirtualKeyRequest, so this resource keeps
// the singular shape until a multi-budget HCL schema is designed. Tracking
// follow-up: support multi-budget per VK and per provider config.
//
// apiRespToBudgetModel converts a budget from the API response to TF state.
// The API may not faithfully echo back calendar_aligned, so we preserve the
// prior (plan) value when the response omits it or returns the zero value.
func apiRespToBudgetModel(b *bifrostclient.VKBudget, prior *VKBudgetModel) *VKBudgetModel {
	if b == nil {
		return nil
	}
	calAligned := types.BoolValue(b.CalendarAligned)
	if !b.CalendarAligned && prior != nil && prior.CalendarAligned.ValueBool() {
		calAligned = prior.CalendarAligned
	}
	return &VKBudgetModel{
		MaxLimit:        types.Float64Value(b.MaxLimit),
		ResetDuration:   types.StringValue(b.ResetDuration),
		CalendarAligned: calAligned,
	}
}

func apiRespToRateLimitModel(rl *bifrostclient.VKRateLimit) *VKRateLimitModel {
	if rl == nil {
		return nil
	}
	m := &VKRateLimitModel{
		TokenMaxLimit:        types.Int64Null(),
		TokenResetDuration:   types.StringNull(),
		RequestMaxLimit:      types.Int64Null(),
		RequestResetDuration: types.StringNull(),
	}
	if rl.TokenMaxLimit != nil {
		m.TokenMaxLimit = types.Int64Value(*rl.TokenMaxLimit)
	}
	if rl.TokenResetDuration != nil {
		m.TokenResetDuration = types.StringValue(*rl.TokenResetDuration)
	}
	if rl.RequestMaxLimit != nil {
		m.RequestMaxLimit = types.Int64Value(*rl.RequestMaxLimit)
	}
	if rl.RequestResetDuration != nil {
		m.RequestResetDuration = types.StringValue(*rl.RequestResetDuration)
	}
	return m
}

// apiRespToVKProviderConfigModel maps an API provider-config entry into TF state.
// key_ids are not returned by Bifrost, so they are preserved from prior state to
// avoid perpetual drift.
func apiRespToVKProviderConfigModel(pc bifrostclient.VKProviderConfigResponse, prior VKProviderConfigModel) VKProviderConfigModel {
	m := VKProviderConfigModel{
		Provider: types.StringValue(pc.Provider),
		KeyIDs:   prior.KeyIDs,
	}
	if pc.Weight != nil {
		m.Weight = types.Float64Value(*pc.Weight)
	}
	for _, am := range pc.AllowedModels {
		m.AllowedModels = append(m.AllowedModels, types.StringValue(am))
	}
	m.Budget = apiRespToBudgetModel(pc.Budget, prior.Budget)
	m.RateLimit = apiRespToRateLimitModel(pc.RateLimit)
	return m
}
