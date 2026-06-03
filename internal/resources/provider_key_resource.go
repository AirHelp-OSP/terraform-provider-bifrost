package resources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
var _ resource.ResourceWithUpgradeState = &ProviderKeyResource{}

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
	ValueSHA256      types.String           `tfsdk:"value_sha256"`
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
	attrs := providerKeyBaseAttributes()

	// `value` is a write-only argument: Terraform sends it to Bifrost but never
	// persists it to state. value_sha256 is the stored digest that drives change
	// detection in its place. Write-only attributes require Terraform >= 1.11
	// (OpenTofu >= 1.10).
	attrs["value"] = schema.StringAttribute{
		MarkdownDescription: "The API key value, supplied as a [write-only argument]" +
			"(https://developer.hashicorp.com/terraform/language/resources/ephemeral/write-only): " +
			"it is sent to Bifrost but **never stored in Terraform state**. Changing it updates the " +
			"computed `value_sha256`, which is what drives the diff. Supports `env.VAR_NAME` references. " +
			"Optional because some providers (notably AWS Bedrock) ignore the field — credentials live " +
			"in the provider-specific block (`bedrock_key_config`) instead. **Requires Terraform >= 1.11 " +
			"/ OpenTofu >= 1.10.**",
		Description: "The API key value (write-only, never stored in state). Optional; some providers (e.g. Bedrock) ignore it.",
		Optional:    true,
		Sensitive:   true,
		WriteOnly:   true,
	}
	attrs["value_sha256"] = schema.StringAttribute{
		MarkdownDescription: "SHA-256 hex digest of `value`, stored in place of the plaintext secret. " +
			"Terraform compares this digest across plans to detect when the key value changes (the secret " +
			"itself is never written to state). Null when no `value` is set.",
		Description: "SHA-256 digest of value; drives change detection without storing the secret.",
		Computed:    true,
		PlanModifiers: []planmodifier.String{
			valueSHA256Hash(),
		},
	}

	resp.Schema = schema.Schema{
		Version: 1,
		MarkdownDescription: "Manages a single API key on a [Bifrost provider](https://github.com/maximhq/bifrost). " +
			"Backed by Bifrost v1.5.0's per-key endpoints (`/api/providers/{provider}/keys`). " +
			"Reference the parent provider via `provider_name = bifrost_provider.X.provider_name`. " +
			"The secret is supplied via the write-only `value` argument and is never stored in state.",
		Description: "Manages a single API key on a Bifrost provider (v1.5.0+).",
		Attributes:  attrs,
	}
}

// providerKeyBaseAttributes returns the attributes common to every schema
// version — everything except the secret value, which changed shape in v1
// (plaintext `value` → write-only `value_wo` + computed `value_sha256`).
// A fresh map is returned on each call so callers can add version-specific
// attributes without mutating a shared instance.
func providerKeyBaseAttributes() map[string]schema.Attribute {
	allowAllModels, _ := types.ListValue(types.StringType, []attr.Value{types.StringValue("*")})

	return map[string]schema.Attribute{
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
	}
}

// ── Plan modifiers ─────────────────────────────────────────────────────────────

// valueSHA256PlanModifier keeps the computed value_sha256 in sync with the
// write-only `value`. Because the secret is never stored, the digest is the
// only signal Terraform has to detect a changed key: a different value hashes
// to a different digest, which surfaces as a diff and drives Update.
type valueSHA256PlanModifier struct{}

// valueSHA256Hash returns the plan modifier that recomputes value_sha256 from
// the configured value.
func valueSHA256Hash() planmodifier.String { return valueSHA256PlanModifier{} }

func (valueSHA256PlanModifier) Description(_ context.Context) string {
	return "Sets value_sha256 to the SHA-256 digest of value so a changed secret produces a diff."
}

func (m valueSHA256PlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (valueSHA256PlanModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// `value` lives in the config (write-only values are never in plan/state),
	// so read it from there. Plan modifiers receive the full resource config.
	var value types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("value"), &value)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// An unknown secret (e.g. sourced from another not-yet-created resource)
	// must plan as unknown so the digest computed at apply time is consistent.
	if value.IsUnknown() {
		resp.PlanValue = types.StringUnknown()
		return
	}
	resp.PlanValue = sha256OfValue(value)
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
	var plan, config ProviderKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	providerName := plan.ProviderName.ValueString()
	tflog.Debug(ctx, "creating Bifrost provider key", map[string]any{
		"provider_name": providerName,
		"name":          plan.Name.ValueString(),
	})

	// `value` is write-only, so it is null in the plan — read it from config.
	apiKey, diags := providerKeyModelToAPI(ctx, &plan, config.Value, "")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResp, err := r.client.CreateProviderKey(ctx, providerName, apiKey)
	if err != nil {
		// A name collision means the key exists server-side but isn't tracked in
		// state (e.g. a prior apply created it but never persisted state). We
		// cannot create it again — point the user at import instead of surfacing
		// the raw 500.
		if bifrostclient.IsAlreadyExists(err) {
			resp.Diagnostics.AddError(
				"Provider key already exists",
				fmt.Sprintf(
					"A key named %q already exists on Bifrost provider %q but is not tracked in "+
						"Terraform state, so it cannot be created again. Import the existing key instead:\n\n"+
						"  terraform import bifrost_provider_key.<name> %s:%s",
					plan.Name.ValueString(), providerName, providerName, plan.Name.ValueString()),
			)
			return
		}
		resp.Diagnostics.AddError("Error creating provider key", err.Error())
		return
	}

	newState := apiKeyToProviderKeyModel(apiResp, &plan, providerName)
	newState.ValueSHA256 = sha256OfValue(config.Value)
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

	// Read has no config, so the secret digest cannot be recomputed; carry the
	// prior value_sha256 forward (apiKeyToProviderKeyModel preserves it).
	newState := apiKeyToProviderKeyModel(apiResp, &state, providerName)
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *ProviderKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state, config ProviderKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	providerName := state.ProviderName.ValueString()
	keyID := state.KeyID.ValueString()
	tflog.Debug(ctx, "updating Bifrost provider key", map[string]any{
		"provider_name": providerName,
		"key_id":        keyID,
	})

	apiKey, diags := providerKeyModelToAPI(ctx, &plan, config.Value, keyID)
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
	newState.ValueSHA256 = sha256OfValue(config.Value)
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

// ── State upgrade ──────────────────────────────────────────────────────────────

// providerKeyResourceModelV0 mirrors the v0 state shape, where the secret was
// stored as a plaintext `value` attribute. v1 keeps the same `value` argument
// name but makes it write-only (never stored) and adds the computed
// `value_sha256` digest.
type providerKeyResourceModelV0 struct {
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

// providerKeySchemaV0 reconstructs the v0 schema so the framework can decode
// prior state during the upgrade. In v0 `value` was a regular (stored)
// attribute; v1 makes it write-only and adds `value_sha256`.
func providerKeySchemaV0() *schema.Schema {
	attrs := providerKeyBaseAttributes()
	attrs["value"] = schema.StringAttribute{
		Description: "The API key value (sensitive, redacted on read).",
		Optional:    true,
		Sensitive:   true,
	}
	return &schema.Schema{Version: 0, Attributes: attrs}
}

func (r *ProviderKeyResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		// v0 → v1: drop the plaintext `value` from state and replace it with the
		// SHA-256 digest in `value_sha256`. The secret survives only as a hash;
		// `value` is now write-only, so it stays null in state.
		0: {
			PriorSchema:   providerKeySchemaV0(),
			StateUpgrader: upgradeProviderKeyStateV0toV1,
		},
	}
}

func upgradeProviderKeyStateV0toV1(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
	var old providerKeyResourceModelV0
	resp.Diagnostics.Append(req.State.Get(ctx, &old)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, upgradeProviderKeyModelV0toV1(old))...)
}

// upgradeProviderKeyModelV0toV1 carries every field forward unchanged except
// the secret: the plaintext `value` becomes the `value_sha256` digest, and the
// now write-only `value` is left null (write-only values are never persisted).
func upgradeProviderKeyModelV0toV1(old providerKeyResourceModelV0) ProviderKeyResourceModel {
	return ProviderKeyResourceModel{
		ID:               old.ID,
		ProviderName:     old.ProviderName,
		Name:             old.Name,
		KeyID:            old.KeyID,
		Models:           old.Models,
		ModelAliases:     old.ModelAliases,
		Weight:           old.Weight,
		Enabled:          old.Enabled,
		BedrockKeyConfig: old.BedrockKeyConfig,
		ValueSHA256:      sha256OfValue(old.Value),
	}
}

// ── Conversion ───────────────────────────────────────────────────────────────

// sha256OfValue returns the hex-encoded SHA-256 digest of a write-only secret,
// or null when the secret is unset/empty. Folding empty/null into null keeps an
// omitted value (e.g. a Bedrock key authenticating via bedrock_key_config) from
// round-tripping as the digest of an empty string.
func sha256OfValue(v types.String) types.String {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return types.StringNull()
	}
	sum := sha256.Sum256([]byte(v.ValueString()))
	return types.StringValue(hex.EncodeToString(sum[:]))
}

// providerKeyModelToAPI builds a schemas.Key from the plan model. keyID is
// empty on Create and the state's UUID on Update. value is the write-only
// secret read from the resource config (null when the user omits it).
//
// Value handling matrix:
//   - known plaintext → wrap and send (regular path).
//   - null            → empty EnvVar; the field is Optional because providers
//     like AWS Bedrock authenticate via the provider-specific block instead.
//   - unknown         → diag error: the secret depends on a not-yet-created
//     resource. There is no prior plaintext to fall back on (it is never
//     stored), so refuse rather than send a blank value.
func providerKeyModelToAPI(ctx context.Context, m *ProviderKeyResourceModel, value types.String, keyID string) (schemas.Key, diag.Diagnostics) {
	var diags diag.Diagnostics
	k := schemas.Key{
		ID:     keyID,
		Name:   m.Name.ValueString(),
		Weight: m.Weight.ValueFloat64(),
	}

	switch {
	case value.IsUnknown():
		diags.AddAttributeError(
			path.Root("value"),
			"Unknown value at apply time",
			"`value` resolved to an unknown at apply time. This usually means the attribute "+
				"depends on a resource that has not yet been created. Because the secret is never "+
				"stored in state, there is no prior value to fall back on — provide an explicit "+
				"value or depend on a known attribute.",
		)
		return k, diags
	case value.IsNull():
		// Leave k.Value as the zero EnvVar — Bifrost stores it as empty and
		// providers like Bedrock ignore the field entirely.
	default:
		k.Value = *schemas.NewEnvVar(value.ValueString())
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
		k.BedrockKeyConfig = modelToBedrockKeyConfig(m.BedrockKeyConfig)
	}

	return k, diags
}

// apiKeyToProviderKeyModel projects a schemas.Key into TF state, preserving
// sensitive fields from prior state when the API redacts them. value_sha256 is
// a Terraform-side digest the API never returns, so it is carried over from
// prior state here; Create/Update overwrite it with the digest of the secret
// they just sent.
func apiKeyToProviderKeyModel(apiKey *schemas.Key, prior *ProviderKeyResourceModel, providerName string) *ProviderKeyResourceModel {
	m := &ProviderKeyResourceModel{
		ID:           types.StringValue(providerName + ":" + apiKey.Name),
		ProviderName: types.StringValue(providerName),
		Name:         types.StringValue(apiKey.Name),
		KeyID:        types.StringValue(apiKey.ID),
		Weight:       types.Float64Value(apiKey.Weight),
	}

	// value_sha256: preserve prior digest (Read). Value stays null — it is
	// write-only and must never be written to state.
	if prior != nil {
		m.ValueSHA256 = prior.ValueSHA256
	} else {
		m.ValueSHA256 = types.StringNull()
	}

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
		m.BedrockKeyConfig = bedrockKeyConfigToModel(apiKey.BedrockKeyConfig, priorBedrock)
	}

	return m
}
