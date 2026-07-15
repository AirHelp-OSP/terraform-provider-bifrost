package resources

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/maximhq/bifrost/core/schemas"
)

// wildcardNotMixedValidator enforces the v1.5.0 BC4 contract: a whitelist
// containing "*" cannot also contain other values (Bifrost returns HTTP 400).
// Mirroring this at plan time saves a round-trip and surfaces a clearer error.
type wildcardNotMixedValidator struct{}

// WildcardNotMixed returns a validator that rejects a list mixing "*" with
// specific values (e.g. ["*", "gpt-4o"]).
func WildcardNotMixed() validator.List {
	return wildcardNotMixedValidator{}
}

func (wildcardNotMixedValidator) Description(_ context.Context) string {
	return "wildcard '*' cannot be mixed with other values"
}

func (v wildcardNotMixedValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (wildcardNotMixedValidator) ValidateList(ctx context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var vals []string
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &vals, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	hasWildcard := false
	for _, s := range vals {
		if s == "*" {
			hasWildcard = true
			break
		}
	}
	if hasWildcard && len(vals) > 1 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid whitelist",
			"Bifrost v1.5.0 rejects lists that mix '*' with specific values. "+
				"Use [\"*\"] alone to allow all, or list specific entries without '*'.",
		)
	}
}

// envVarToString converts a *schemas.SecretVar from an API response into a
// types.String for state.
//
// Bifrost redacts these fields on read, so the API echo is never an
// authoritative source of truth for them. The projection is therefore
// prior-first:
//
//  1. Known prior (non-null, non-unknown) → return prior. During Create/Update
//     prior is the plan; during Read it is existing state. Either way it is the
//     value the resource must settle on, so returning it keeps applied state ==
//     plan and avoids Terraform's "inconsistent values for sensitive attribute"
//     abort — including when the response carries a redacted string, an empty
//     value, a value the server rewrote, or a legacy JSON secret blob (below).
//  2. Otherwise (no prior on import/first Read, or an unknown Computed prior that
//     must resolve at apply) and a genuine, non-placeholder value → adopt the
//     API value.
//  3. Otherwise and a placeholder — nil, redacted, empty, or a legacy secret
//     blob → types.StringNull(). Storing a placeholder would poison state or
//     produce a spurious "" → null diff against a config that omits the field.
//
// An unknown prior must NOT be returned verbatim: a Computed attribute (e.g.
// network_config.ca_cert_pem) plans as unknown, and every value must be known
// after apply, so it has to be resolved from the API response here.
//
// Empty values are treated as placeholders (not adopted) because
// SecretVar.IsRedacted() deliberately returns false for empty values; without
// this fold an Optional field the user never set would round-trip as
// `null → ""` on every plan.
//
// A "legacy secret blob" is the raw JSON serialization of a SecretVar/EnvVar
// (e.g. `{"value":"...","type":"plain_text"}`) that a pre-v1.6 provider (core
// v1.5.x) stored as the field value: its EnvVar.UnmarshalJSON only recognized
// the `{value,env_var}` shape, so the v1.6 `{value,type}` wire form fell
// through and the whole blob was kept as the value. Such a blob is a placeholder
// and must never be adopted into state.
func envVarToString(ev *schemas.SecretVar, prior types.String) types.String {
	if !prior.IsNull() && !prior.IsUnknown() {
		return prior
	}
	if ev == nil || ev.IsRedacted() || ev.GetValue() == "" || looksLikeLegacySecretBlob(ev.GetValue()) {
		return types.StringNull()
	}
	return types.StringValue(ev.GetValue())
}

// looksLikeLegacySecretBlob reports whether s is the JSON serialization of a
// SecretVar/EnvVar object rather than a bare secret value — i.e. a JSON object
// carrying a "value" key. See envVarToString for how a pre-v1.6 provider came to
// store such blobs in state.
func looksLikeLegacySecretBlob(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return false
	}
	_, hasValue := obj["value"]
	return hasValue
}

// emptyStringAsNull mirrors envVarToString's null-coalescing for plain
// (non-EnvVar) string fields: the Bifrost API returns "" for unconfigured
// optional strings such as NetworkConfig.BaseURL, but Terraform plans
// resolve an omitted Optional attribute to null. Storing "" would create
// a permanent "" → null diff after import or after a no-op apply.
func emptyStringAsNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// stringToEnvVar converts a TF state/plan types.String into a *schemas.EnvVar
// for sending in API requests. Null/unknown become nil so the field is omitted
// from JSON marshaling (each schema field already uses `omitempty`).
func stringToEnvVar(s types.String) *schemas.SecretVar {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	return schemas.NewSecretVar(s.ValueString())
}

// ── BedrockKeyConfig (Bifrost v1.5.0) ─────────────────────────────────────────
//
// v1.5.0 removed BedrockKeyConfig.Deployments — model alias mappings now live
// on the top-level Key.Aliases field (exposed in TF as `model_aliases`).
// The remaining fields are *schemas.EnvVar pointers, supporting env.* refs.

// BedrockKeyConfigModel mirrors the v1.5.0 Bedrock-specific key configuration.
type BedrockKeyConfigModel struct {
	AccessKey       types.String `tfsdk:"access_key"`
	SecretKey       types.String `tfsdk:"secret_key"`
	SessionToken    types.String `tfsdk:"session_token"`
	Region          types.String `tfsdk:"region"`
	ARN             types.String `tfsdk:"arn"`
	RoleARN         types.String `tfsdk:"role_arn"`
	ExternalID      types.String `tfsdk:"external_id"`
	RoleSessionName types.String `tfsdk:"role_session_name"`
}

func bedrockKeyConfigToModel(bkc *schemas.BedrockKeyConfig, prior *BedrockKeyConfigModel) *BedrockKeyConfigModel {
	// Bifrost redacts every bedrock_key_config field on read, so the API echo is
	// never an authoritative source of truth for them. Whenever we have a prior
	// model it is exactly the value the resource must settle on — the plan during
	// Create/Update, existing state during Read — so return it verbatim.
	//
	// This is what keeps applied state == plan and avoids Terraform's
	// ".bedrock_key_config: inconsistent values for sensitive attribute" abort. It
	// holds no matter what the server returns for a field the config omits (plan
	// null): a redacted string, an empty value, a raw non-redacted credential
	// (some Bifrost deployments echo stored creds back verbatim on the update
	// response), or a legacy JSON secret blob written by a pre-v1.6 provider.
	// Per-field projection cannot guarantee this — a non-redacted echo of an
	// omitted credential would be adopted over the planned null.
	if prior != nil {
		return prior
	}

	// No prior (first Read after ImportState): project from the API, folding
	// redacted/empty/blob placeholders to null.
	nul := types.StringNull()
	return &BedrockKeyConfigModel{
		// AccessKey / SecretKey are value types (SecretVar, not *SecretVar).
		AccessKey:       envVarToString(&bkc.AccessKey, nul),
		SecretKey:       envVarToString(&bkc.SecretKey, nul),
		SessionToken:    envVarToString(bkc.SessionToken, nul),
		Region:          envVarToString(bkc.Region, nul),
		ARN:             envVarToString(bkc.ARN, nul),
		RoleARN:         envVarToString(bkc.RoleARN, nul),
		ExternalID:      envVarToString(bkc.ExternalID, nul),
		RoleSessionName: envVarToString(bkc.RoleSessionName, nul),
	}
}

func modelToBedrockKeyConfig(m *BedrockKeyConfigModel) *schemas.BedrockKeyConfig {
	bkc := &schemas.BedrockKeyConfig{}

	if !m.AccessKey.IsNull() && !m.AccessKey.IsUnknown() {
		bkc.AccessKey = *schemas.NewSecretVar(m.AccessKey.ValueString())
	}
	if !m.SecretKey.IsNull() && !m.SecretKey.IsUnknown() {
		bkc.SecretKey = *schemas.NewSecretVar(m.SecretKey.ValueString())
	}
	bkc.SessionToken = stringToEnvVar(m.SessionToken)
	bkc.Region = stringToEnvVar(m.Region)
	bkc.ARN = stringToEnvVar(m.ARN)
	bkc.RoleARN = stringToEnvVar(m.RoleARN)
	bkc.ExternalID = stringToEnvVar(m.ExternalID)
	bkc.RoleSessionName = stringToEnvVar(m.RoleSessionName)

	return bkc
}

// ── Model aliases (Bifrost v1.6.x AliasConfig) ────────────────────────────────
//
// Bifrost v1.6.0 replaced the string-valued alias map with a rich AliasConfig
// (KeyAliases = map[string]AliasConfig). An alias now maps a user-facing model
// name to a wire model id plus optional routing/pricing metadata and
// provider-specific overrides — notably Bedrock's inference_profile_arn. The
// wire format stays backward compatible: an alias carrying only model_id
// marshals back to a plain string.

// modelFamilyValues enumerates the valid schemas.ModelFamily values, surfaced as
// the OneOf set for the `model_family` attribute so a typo is caught at plan time
// rather than by the server (mirrors schemas.ModelFamily.IsValid).
var modelFamilyValues = []string{
	string(schemas.ModelFamilyAnthropic),
	string(schemas.ModelFamilyOpenAI),
	string(schemas.ModelFamilyMistral),
	string(schemas.ModelFamilyCohere),
	string(schemas.ModelFamilyGemini),
	string(schemas.ModelFamilyGemma),
	string(schemas.ModelFamilyLlama),
	string(schemas.ModelFamilyImagen),
	string(schemas.ModelFamilyVeo),
	string(schemas.ModelFamilyNova),
	string(schemas.ModelFamilyTitan),
}

// AliasConfigModel is the nested-object value of the `model_aliases` map.
type AliasConfigModel struct {
	ModelID             types.String `tfsdk:"model_id"`
	InferenceProfileARN types.String `tfsdk:"inference_profile_arn"`
	ModelName           types.String `tfsdk:"model_name"`
	ModelFamily         types.String `tfsdk:"model_family"`
	Description         types.String `tfsdk:"description"`
	Region              types.String `tfsdk:"region"`
}

// aliasConfigAttrTypes is the attribute-type map for a single alias object. It is
// the single source of truth shared by the schema, the map element type, and
// every types.ObjectValue construction below.
func aliasConfigAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"model_id":              types.StringType,
		"inference_profile_arn": types.StringType,
		"model_name":            types.StringType,
		"model_family":          types.StringType,
		"description":           types.StringType,
		"region":                types.StringType,
	}
}

// aliasConfigObjectType is the element type of the `model_aliases` map.
func aliasConfigObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: aliasConfigAttrTypes()}
}

// modelAliasesToAPI converts the nested `model_aliases` map into
// schemas.KeyAliases. Region and inference_profile_arn are SecretVars so they
// support env./vault. references; every other field is plain. Null/unknown
// optional fields are omitted so an alias with only model_id round-trips to the
// legacy string wire form.
func modelAliasesToAPI(ctx context.Context, m types.Map) (schemas.KeyAliases, diag.Diagnostics) {
	var diags diag.Diagnostics
	if m.IsNull() || m.IsUnknown() {
		return nil, diags
	}

	elems := make(map[string]AliasConfigModel, len(m.Elements()))
	diags.Append(m.ElementsAs(ctx, &elems, false)...)
	if diags.HasError() {
		return nil, diags
	}

	ka := make(schemas.KeyAliases, len(elems))
	for name, a := range elems {
		ac := schemas.AliasConfig{ModelID: a.ModelID.ValueString()}

		if !a.ModelName.IsNull() && !a.ModelName.IsUnknown() {
			v := a.ModelName.ValueString()
			ac.ModelName = &v
		}
		if !a.ModelFamily.IsNull() && !a.ModelFamily.IsUnknown() {
			f := schemas.ModelFamily(a.ModelFamily.ValueString())
			ac.ModelFamily = &f
		}
		if !a.Description.IsNull() && !a.Description.IsUnknown() {
			ac.Description = a.Description.ValueString()
		}
		if !a.Region.IsNull() && !a.Region.IsUnknown() {
			ac.Region = schemas.NewSecretVar(a.Region.ValueString())
		}
		if !a.InferenceProfileARN.IsNull() && !a.InferenceProfileARN.IsUnknown() {
			ac.BedrockAliasCfg = &schemas.BedrockAliasCfg{
				InferenceProfileARN: schemas.NewSecretVar(a.InferenceProfileARN.ValueString()),
			}
		}

		ka[name] = ac
	}
	return ka, diags
}

// apiAliasesToModel projects schemas.KeyAliases into the nested `model_aliases`
// map. Empty aliases become MapNull so an Optional+unset attribute round-trips
// cleanly. The SecretVar fields (region, inference_profile_arn) reuse
// envVarToString so a redacted env./vault. reference falls back to the prior
// state value rather than being clobbered. prior is the prior `model_aliases`
// map (may be null), used only to recover redacted secret references.
func apiAliasesToModel(ctx context.Context, aliases schemas.KeyAliases, prior types.Map) types.Map {
	objType := aliasConfigObjectType()
	if len(aliases) == 0 {
		return types.MapNull(objType)
	}

	priorAliases := map[string]AliasConfigModel{}
	if !prior.IsNull() && !prior.IsUnknown() {
		// Best-effort: on decode failure we simply have no prior to preserve.
		_ = prior.ElementsAs(ctx, &priorAliases, false)
	}

	elems := make(map[string]attr.Value, len(aliases))
	for name, ac := range aliases {
		var p *AliasConfigModel
		if pv, ok := priorAliases[name]; ok {
			pv := pv
			p = &pv
		}
		elems[name] = aliasConfigToObjectValue(ac, p)
	}
	return types.MapValueMust(objType, elems)
}

// aliasConfigToObjectValue projects a single AliasConfig into a types.Object.
// prior (may be nil) supplies fallback values for redacted secret references.
func aliasConfigToObjectValue(ac schemas.AliasConfig, prior *AliasConfigModel) types.Object {
	priorOf := func(get func(*AliasConfigModel) types.String) types.String {
		if prior == nil {
			return types.StringNull()
		}
		return get(prior)
	}

	region := types.StringNull()
	if ac.Region != nil {
		region = envVarToString(ac.Region, priorOf(func(p *AliasConfigModel) types.String { return p.Region }))
	}

	inferenceProfileARN := types.StringNull()
	if ac.BedrockAliasCfg != nil {
		// InferenceProfileARN is promoted from the embedded *BedrockAliasCfg.
		inferenceProfileARN = envVarToString(ac.InferenceProfileARN,
			priorOf(func(p *AliasConfigModel) types.String { return p.InferenceProfileARN }))
	}

	return types.ObjectValueMust(aliasConfigAttrTypes(), map[string]attr.Value{
		"model_id":              types.StringValue(ac.ModelID),
		"inference_profile_arn": inferenceProfileARN,
		"model_name":            stringPtrToString(ac.ModelName),
		"model_family":          modelFamilyToString(ac.ModelFamily),
		"description":           emptyStringAsNull(ac.Description),
		"region":                region,
	})
}

// stringPtrToString converts an optional *string into a types.String.
func stringPtrToString(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

// modelFamilyToString converts an optional *schemas.ModelFamily into a types.String.
func modelFamilyToString(f *schemas.ModelFamily) types.String {
	if f == nil {
		return types.StringNull()
	}
	return types.StringValue(string(*f))
}
