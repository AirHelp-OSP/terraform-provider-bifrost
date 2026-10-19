package resources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	bifrostclient "github.com/airhelp-osp/terraform-provider-bifrost/internal/client"
)

var _ resource.Resource = &ModelLimitResource{}
var _ resource.ResourceWithImportState = &ModelLimitResource{}

// NewModelLimitResource returns a new ModelLimitResource.
func NewModelLimitResource() resource.Resource {
	return &ModelLimitResource{}
}

// ModelLimitResource manages bifrost_model_limit resources (Bifrost model configs).
type ModelLimitResource struct {
	client *bifrostclient.BifrostClient
}

type ModelLimitResourceModel struct {
	ID        types.String      `tfsdk:"id"`
	ModelName types.String      `tfsdk:"model_name"`
	Provider  types.String      `tfsdk:"provider_name"`
	Scope     types.String      `tfsdk:"scope"`
	ScopeID   types.String      `tfsdk:"scope_id"`
	Budgets   []VKBudgetModel   `tfsdk:"budgets"`
	RateLimit *VKRateLimitModel `tfsdk:"rate_limit"`
}

func (r *ModelLimitResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model_limit"
}

func (r *ModelLimitResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Bifrost model limit (model config): standalone budgets and a rate limit " +
			"applied to a model, optionally narrowed to a provider, globally or within a scope.\n\n" +
			"~> **Virtual-key scope:** Bifrost stores a virtual key's own `budgets` as a `virtual_key`-scoped " +
			"model limit with `model_name = \"*\"`, so do not declare that combination here. Per-model limits " +
			"scoped to a virtual key are deleted by Bifrost when the key no longer lists their provider in " +
			"`provider_configs`.",
		Description: "Manages a Bifrost model limit (standalone budgets and rate limit for a model).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "UUID assigned by Bifrost at creation time.",
				Description:         "UUID assigned by Bifrost.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"model_name": schema.StringAttribute{
				MarkdownDescription: "Model the limit applies to. Use `*` to match all models.",
				Description:         "Model the limit applies to ('*' for all models).",
				Required:            true,
			},
			"provider_name": schema.StringAttribute{
				MarkdownDescription: "Provider name (e.g. `openai`). Omit to apply to the model on every provider. " +
					"Changing it forces a new resource.",
				Description: "Provider name; omit to apply to all providers.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"scope": schema.StringAttribute{
				MarkdownDescription: "Where the limit applies: `global`, `virtual_key`, or `project` " +
					"(_enterprise_). Defaults to `global`. Changing it forces a new resource.",
				Description: "Where the limit applies: global, virtual_key or project.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("global"),
				Validators: []validator.String{
					stringvalidator.OneOf("global", "virtual_key", "project"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"scope_id": schema.StringAttribute{
				MarkdownDescription: "ID of the scope target (virtual key or project ID). Required when `scope` " +
					"is not `global`. Changing it forces a new resource.",
				Description: "ID of the scope target; required when scope is not global.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"budgets":    budgetsSchema,
			"rate_limit": rateLimitSchema,
		},
	}
}

func (r *ModelLimitResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ModelLimitResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ModelLimitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "creating Bifrost model limit", map[string]any{"model_name": plan.ModelName.ValueString()})

	apiResp, err := r.client.CreateModelConfig(ctx, modelLimitToCreateRequest(plan))
	if err != nil {
		if bifrostclient.IsConflict(err) {
			resp.Diagnostics.AddError(
				"Model limit already exists",
				fmt.Sprintf("A model limit for model %q with the same provider and scope already exists in Bifrost "+
					"but is not tracked in Terraform state. Import it instead:\n\n"+
					"  terraform import bifrost_model_limit.<name> <id>\n\n%s",
					plan.ModelName.ValueString(), err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError("Error creating model limit", err.Error())
		return
	}

	newState := modelLimitResponseToModel(apiResp, &plan)

	tflog.Debug(ctx, "created Bifrost model limit", map[string]any{"id": newState.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *ModelLimitResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ModelLimitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResp, err := r.client.GetModelConfig(ctx, state.ID.ValueString())
	if err != nil {
		if bifrostclient.IsNotFound(err) {
			tflog.Debug(ctx, "Bifrost model limit not found, removing from state",
				map[string]any{"id": state.ID.ValueString()})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading model limit", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelLimitResponseToModel(apiResp, &state))...)
}

func (r *ModelLimitResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ModelLimitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state ModelLimitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "updating Bifrost model limit", map[string]any{"id": state.ID.ValueString()})

	apiResp, err := r.client.UpdateModelConfig(ctx, state.ID.ValueString(), modelLimitToUpdateRequest(plan))
	if err != nil {
		resp.Diagnostics.AddError("Error updating model limit", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelLimitResponseToModel(apiResp, &plan))...)
}

func (r *ModelLimitResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ModelLimitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "deleting Bifrost model limit", map[string]any{"id": state.ID.ValueString()})

	err := r.client.DeleteModelConfig(ctx, state.ID.ValueString())
	if err != nil && !bifrostclient.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting model limit", err.Error())
	}
}

func (r *ModelLimitResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func modelLimitToCreateRequest(m ModelLimitResourceModel) bifrostclient.CreateModelConfigRequest {
	return bifrostclient.CreateModelConfigRequest{
		ModelName: m.ModelName.ValueString(),
		Provider:  m.Provider.ValueStringPointer(),
		Scope:     m.Scope.ValueString(),
		ScopeID:   m.ScopeID.ValueStringPointer(),
		Budgets:   modelToBudgets(m.Budgets),
		RateLimit: modelToRateLimit(m.RateLimit),
	}
}

// modelLimitToUpdateRequest always sends budgets and rate_limit so removing
// them from config clears them server-side instead of being a silent no-op.
func modelLimitToUpdateRequest(m ModelLimitResourceModel) bifrostclient.UpdateModelConfigRequest {
	req := bifrostclient.UpdateModelConfigRequest{
		ModelName: m.ModelName.ValueStringPointer(),
		Provider:  m.Provider.ValueStringPointer(),
		Budgets:   modelToBudgets(m.Budgets),
		RateLimit: modelToRateLimit(m.RateLimit),
	}
	if req.Budgets == nil {
		req.Budgets = []bifrostclient.VKBudget{}
	}
	if req.RateLimit == nil {
		req.RateLimit = &bifrostclient.VKRateLimit{}
	}
	return req
}

func modelLimitResponseToModel(apiResp *bifrostclient.ModelConfigResponse, prior *ModelLimitResourceModel) *ModelLimitResourceModel {
	m := &ModelLimitResourceModel{
		ID:        types.StringValue(apiResp.ID),
		ModelName: types.StringValue(apiResp.ModelName),
		Provider:  types.StringPointerValue(apiResp.Provider),
		Scope:     types.StringValue(apiResp.Scope),
		ScopeID:   types.StringPointerValue(apiResp.ScopeID),
	}
	var priorBudgets []VKBudgetModel
	var priorRateLimit *VKRateLimitModel
	if prior != nil {
		priorBudgets = prior.Budgets
		priorRateLimit = prior.RateLimit
	}
	m.Budgets = apiRespToBudgets(apiResp.Budgets, priorBudgets)
	m.RateLimit = apiRespToRateLimitModel(apiResp.RateLimit, priorRateLimit)
	return m
}
