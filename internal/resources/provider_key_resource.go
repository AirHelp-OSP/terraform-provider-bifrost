package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/maximhq/bifrost/core/schemas"

	bifrostclient "github.com/airhelp-osp/terraform-provider-bifrost/internal/client"
)

var _ resource.Resource = &ProviderKeyResource{}
var _ resource.ResourceWithImportState = &ProviderKeyResource{}

// NewProviderKeyResource returns a new ProviderKeyResource.
func NewProviderKeyResource() resource.Resource {
	return &ProviderKeyResource{}
}

// ProviderKeyResource manages a single API key on a Bifrost provider.
//
// Backed by Bifrost v1.5.0's dedicated per-key endpoints under
// /api/providers/{provider}/keys; each Terraform resource maps 1:1 to a
// server-side key identified by a stable UUID (key_id). Multiple key
// resources targeting the same parent bifrost_provider are independent.
type ProviderKeyResource struct {
	client *bifrostclient.BifrostClient
}

// ── Model ────────────────────────────────────────────────────────────────────

type ProviderKeyResourceModel struct {
	ID               types.String           `tfsdk:"id"`
	ProviderName     types.String           `tfsdk:"provider_name"`
	Name             types.String           `tfsdk:"name"`
	KeyID            types.String           `tfsdk:"key_id"`
	Value            types.String           `tfsdk:"value"`
	Models           types.List             `tfsdk:"models"`
	ModelAliases     types.Map              `tfsdk:"model_aliases"`
	Weight           types.Float64          `tfsdk:"weight"`
	Enabled          types.Bool             `tfsdk:"enabled"`
	BedrockKeyConfig *BedrockKeyConfigModel `tfsdk:"bedrock_key_config"`
}

// ── Schema ────────────────────────────────────────────────────────────────────

func (r *ProviderKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_provider_key"
}

func (r *ProviderKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	allowAllModels, _ := types.ListValue(types.StringType, []attr.Value{types.StringValue("*")})

	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a single API key on a [Bifrost provider](https://github.com/maximhq/bifrost). " +
			"Backed by Bifrost v1.5.0's per-key endpoints (`/api/providers/{provider}/keys`). " +
			"Reference the parent provider via `provider_name = bifrost_provider.X.provider_name`.",
		Description: "Manages a single API key on a Bifrost provider (v1.5.0+).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Composite identifier `\"<provider_name>:<name>\"`. Used as the import ID.",
				Description:         "Composite identifier <provider_name>:<name>.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"provider_name": schema.StringAttribute{
				MarkdownDescription: "Name of the parent Bifrost provider (must already exist — typically " +
					"`bifrost_provider.X.provider_name`). Forces replacement when changed.",
				Description: "Name of the parent Bifrost provider.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name for the key. Used as the user-visible identifier " +
					"in the composite resource ID. Forces replacement when changed.",
				Description: "Human-readable name for the key.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"key_id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID for the key (used internally to address PUT/DELETE).",
				Description:         "Server-assigned UUID.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "The API key value. Bifrost redacts this on read, so the prior state " +
					"value is preserved across plans. Supports `env.VAR_NAME` references. " +
					"Optional because some providers (notably AWS Bedrock) ignore the field — " +
					"credentials live in the provider-specific block (`bedrock_key_config`) instead.",
				Description: "The API key value (sensitive, redacted on read). Optional; some providers (e.g. Bedrock) ignore it.",
				Optional:    true,
				Sensitive:   true,
			},
			"models": schema.ListAttribute{
				MarkdownDescription: "Models this key may access (whitelist). Use `[\"*\"]` to allow all " +
					"(the default). **Bifrost v1.5.0 changed the empty-list semantic**: `[]` now means " +
					"_deny all_, not _allow all_. Provider validates that `\"*\"` is not mixed with " +
					"specific values.",
				Description: "Models this key may access. ['*'] means all; [] means none.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     listdefault.StaticValue(allowAllModels),
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{
					listvalidator.UniqueValues(),
					WildcardNotMixed(),
				},
			},
			"model_aliases": schema.MapAttribute{
				MarkdownDescription: "Mapping of user-facing model names to provider-specific identifiers " +
					"(e.g. Bedrock inference profile ARNs, Azure deployment names, fine-tuned model IDs). " +
					"Replaces the Bedrock-only `deployments` map from Bifrost v1.4.x.",
				Description: "User-facing model name → provider-specific identifier.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"weight": schema.Float64Attribute{
				MarkdownDescription: "Load-balancing weight relative to other keys on this provider. Defaults to `1.0`.",
				Description:         "Load-balancing weight.",
				Optional:            true,
				Computed:            true,
				Default:             float64default.StaticFloat64(1.0),
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the key is active. Defaults to `true`.",
				Description:         "Whether the key is active.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"bedrock_key_config": schema.SingleNestedAttribute{
				MarkdownDescription: "AWS Bedrock-specific key configuration. Bifrost redacts sensitive " +
					"fields on read; prior state values are preserved on every Read.",
				Description: "AWS Bedrock-specific key configuration.",
				Optional:    true,
				Attributes: map[string]schema.Attribute{
					"access_key": schema.StringAttribute{
						MarkdownDescription: "AWS access key ID.",
						Description:         "AWS access key ID.",
						Optional:            true,
						Sensitive:           true,
					},
					"secret_key": schema.StringAttribute{
						MarkdownDescription: "AWS secret access key.",
						Description:         "AWS secret access key.",
						Optional:            true,
						Sensitive:           true,
					},
					"session_token": schema.StringAttribute{
						MarkdownDescription: "AWS session token for temporary credentials.",
						Description:         "AWS session token for temporary credentials.",
						Optional:            true,
						Sensitive:           true,
					},
					"region": schema.StringAttribute{
						MarkdownDescription: "AWS region (e.g. `us-east-1`).",
						Description:         "AWS region.",
						Optional:            true,
					},
					"arn": schema.StringAttribute{
						MarkdownDescription: "Amazon Resource Name.",
						Description:         "Amazon Resource Name.",
						Optional:            true,
					},
					"role_arn": schema.StringAttribute{
						MarkdownDescription: "IAM role ARN for STS `AssumeRole`.",
						Description:         "IAM role ARN for STS AssumeRole.",
						Optional:            true,
					},
					"external_id": schema.StringAttribute{
						MarkdownDescription: "External ID for STS `AssumeRole`.",
						Description:         "External ID for STS AssumeRole.",
						Optional:            true,
					},
					"role_session_name": schema.StringAttribute{
						MarkdownDescription: "Session name for STS `AssumeRole`.",
						Description:         "Session name for STS AssumeRole.",
						Optional:            true,
					},
				},
			},
		},
	}
}

// ── CRUD ──────────────────────────────────────────────────────────────────────

func (r *ProviderKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ProviderKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ProviderKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	providerName := plan.ProviderName.ValueString()
	tflog.Debug(ctx, "creating Bifrost provider key", map[string]any{
		"provider_name": providerName,
		"name":          plan.Name.ValueString(),
	})

	apiKey, diags := providerKeyModelToAPI(ctx, &plan, "")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResp, err := r.client.CreateProviderKey(ctx, providerName, apiKey)
	if err != nil {
		resp.Diagnostics.AddError("Error creating provider key", err.Error())
		return
	}

	newState := apiKeyToProviderKeyModel(apiResp, &plan, providerName)
	tflog.Debug(ctx, "created Bifrost provider key", map[string]any{
		"id":     newState.ID.ValueString(),
		"key_id": newState.KeyID.ValueString(),
	})
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *ProviderKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ProviderKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	providerName := state.ProviderName.ValueString()
	keyID := state.KeyID.ValueString()
	tflog.Debug(ctx, "reading Bifrost provider key", map[string]any{
		"provider_name": providerName,
		"key_id":        keyID,
	})

	apiResp, err := r.client.GetProviderKey(ctx, providerName, keyID)
	if err != nil {
		if bifrostclient.IsNotFound(err) {
			tflog.Debug(ctx, "Bifrost provider key not found, removing from state",
				map[string]any{"provider_name": providerName, "key_id": keyID})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading provider key", err.Error())
		return
	}

	newState := apiKeyToProviderKeyModel(apiResp, &state, providerName)
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *ProviderKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ProviderKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	providerName := state.ProviderName.ValueString()
	keyID := state.KeyID.ValueString()
	tflog.Debug(ctx, "updating Bifrost provider key", map[string]any{
		"provider_name": providerName,
		"key_id":        keyID,
	})

	apiKey, diags := providerKeyModelToAPI(ctx, &plan, keyID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResp, err := r.client.UpdateProviderKey(ctx, providerName, keyID, apiKey)
	if err != nil {
		resp.Diagnostics.AddError("Error updating provider key", err.Error())
		return
	}

	newState := apiKeyToProviderKeyModel(apiResp, &plan, providerName)
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *ProviderKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ProviderKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	providerName := state.ProviderName.ValueString()
	keyID := state.KeyID.ValueString()
	tflog.Debug(ctx, "deleting Bifrost provider key", map[string]any{
		"provider_name": providerName,
		"key_id":        keyID,
	})

	err := r.client.DeleteProviderKey(ctx, providerName, keyID)
	if err != nil && !bifrostclient.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting provider key", err.Error())
		return
	}
}

// ImportState parses the composite import ID "provider_name:key_name", uses
// ListProviderKeys to resolve the server-assigned UUID, and seeds state so the
// subsequent Read can fully reconcile.
func (r *ProviderKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	providerName, keyName, ok := parseProviderKeyImportID(req.ID)
	if !ok {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected '<provider_name>:<key_name>'; got %q.", req.ID),
		)
		return
	}

	keys, err := r.client.ListProviderKeys(ctx, providerName)
	if err != nil {
		if bifrostclient.IsNotFound(err) {
			resp.Diagnostics.AddError(
				"Provider not found",
				fmt.Sprintf("Bifrost provider %q does not exist.", providerName),
			)
			return
		}
		resp.Diagnostics.AddError("Error listing provider keys for import", err.Error())
		return
	}

	var keyID string
	for _, k := range keys {
		if k.Name == keyName {
			keyID = k.ID
			break
		}
	}
	if keyID == "" {
		resp.Diagnostics.AddError(
			"Key not found",
			fmt.Sprintf("Provider %q has no key named %q.", providerName, keyName),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("provider_name"), providerName)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), keyName)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key_id"), keyID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// parseProviderKeyImportID splits "<provider_name>:<key_name>". Returns false
// for empty halves so callers can surface a clear error.
func parseProviderKeyImportID(id string) (providerName, keyName string, ok bool) {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	if parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// ── Conversion ───────────────────────────────────────────────────────────────

// providerKeyModelToAPI builds a schemas.Key from the plan model. keyID is
// empty on Create and the state's UUID on Update.
func providerKeyModelToAPI(ctx context.Context, m *ProviderKeyResourceModel, keyID string) (schemas.Key, diag.Diagnostics) {
	var diags diag.Diagnostics
	k := schemas.Key{
		ID:     keyID,
		Name:   m.Name.ValueString(),
		Value:  *schemas.NewEnvVar(m.Value.ValueString()),
		Weight: m.Weight.ValueFloat64(),
	}

	if !m.Models.IsNull() && !m.Models.IsUnknown() {
		var models []string
		d := m.Models.ElementsAs(ctx, &models, false)
		diags.Append(d...)
		k.Models = schemas.WhiteList(models)
	}

	if !m.ModelAliases.IsNull() && !m.ModelAliases.IsUnknown() {
		aliases := make(map[string]string)
		d := m.ModelAliases.ElementsAs(ctx, &aliases, false)
		diags.Append(d...)
		k.Aliases = schemas.KeyAliases(aliases)
	}

	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		v := m.Enabled.ValueBool()
		k.Enabled = &v
	}

	if m.BedrockKeyConfig != nil {
		bkc, d := modelToBedrockKeyConfig(ctx, m.BedrockKeyConfig)
		diags.Append(d...)
		if !diags.HasError() {
			k.BedrockKeyConfig = bkc
		}
	}

	return k, diags
}

// apiKeyToProviderKeyModel projects a schemas.Key into TF state, preserving
// sensitive fields from prior state when the API redacts them.
func apiKeyToProviderKeyModel(apiKey *schemas.Key, prior *ProviderKeyResourceModel, providerName string) *ProviderKeyResourceModel {
	m := &ProviderKeyResourceModel{
		ID:           types.StringValue(providerName + ":" + apiKey.Name),
		ProviderName: types.StringValue(providerName),
		Name:         types.StringValue(apiKey.Name),
		KeyID:        types.StringValue(apiKey.ID),
		Weight:       types.Float64Value(apiKey.Weight),
	}

	// Value: preserve prior state on redaction.
	priorValue := types.StringNull()
	if prior != nil {
		priorValue = prior.Value
	}
	m.Value = envVarToString(&apiKey.Value, priorValue)

	// Models: project WhiteList → types.List. Empty list (deny-all) is a valid
	// user intent in v1.5.0, so do not substitute a default here.
	if apiKey.Models != nil {
		elems := make([]attr.Value, len(apiKey.Models))
		for i, s := range apiKey.Models {
			elems[i] = types.StringValue(s)
		}
		m.Models = types.ListValueMust(types.StringType, elems)
	} else {
		m.Models = types.ListValueMust(types.StringType, []attr.Value{})
	}

	// ModelAliases: empty aliases stays null so Optional+unset round-trips cleanly.
	if len(apiKey.Aliases) > 0 {
		elems := make(map[string]attr.Value, len(apiKey.Aliases))
		for k, v := range apiKey.Aliases {
			elems[k] = types.StringValue(v)
		}
		m.ModelAliases = types.MapValueMust(types.StringType, elems)
	} else {
		m.ModelAliases = types.MapNull(types.StringType)
	}

	if apiKey.Enabled != nil {
		m.Enabled = types.BoolValue(*apiKey.Enabled)
	} else {
		m.Enabled = types.BoolValue(true)
	}

	// Bedrock: API redacts sensitive fields; preserve from prior.
	if apiKey.BedrockKeyConfig == nil {
		if prior != nil {
			m.BedrockKeyConfig = prior.BedrockKeyConfig
		}
	} else {
		var priorBedrock *BedrockKeyConfigModel
		if prior != nil {
			priorBedrock = prior.BedrockKeyConfig
		}
		bkc, _ := bedrockKeyConfigToModel(apiKey.BedrockKeyConfig, priorBedrock)
		m.BedrockKeyConfig = bkc
	}

	return m
}
