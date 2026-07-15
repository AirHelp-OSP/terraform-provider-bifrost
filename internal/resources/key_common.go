package resources

import (
	"context"

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

// envVarToString converts a *schemas.EnvVar from an API response into a
// types.String for state. Four cases:
//
//  1. nil EnvVar → types.StringNull() (Optional/unconfigured).
//  2. server-redacted value with a prior state value → preserve prior
//     (avoids overwriting the user's plaintext on every Read).
//  3. server-redacted *or empty* value with no prior state value (typical of
//     the first Read after ImportState, or of an Optional field the user
//     never set — e.g. `value` on a Bedrock provider key) → types.StringNull().
//     Storing the redacted/empty text would either poison state with a
//     placeholder or produce a spurious "" → null diff against an HCL
//     config that omits the attribute.
//  4. otherwise → take the API value.
//
// Empty values are folded into case 2/3 rather than case 4 because
// EnvVar.IsRedacted() deliberately returns false for empty values
// (envvar.go: `if e.Val == "" && !e.FromEnv { return false }`). Without
// this fold, an Optional EnvVar field the user never set would round-trip
// as `null → ""` on every plan.
func envVarToString(ev *schemas.SecretVar, prior types.String) types.String {
	if ev == nil {
		return types.StringNull()
	}
	if ev.IsRedacted() || ev.GetValue() == "" {
		if !prior.IsNull() {
			return prior
		}
		return types.StringNull()
	}
	return types.StringValue(ev.GetValue())
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
	m := &BedrockKeyConfigModel{}

	priorOf := func(get func(*BedrockKeyConfigModel) types.String) types.String {
		if prior == nil {
			return types.StringNull()
		}
		return get(prior)
	}

	// AccessKey / SecretKey are value types (SecretVar, not *SecretVar).
	m.AccessKey = envVarToString(&bkc.AccessKey, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.AccessKey }))
	m.SecretKey = envVarToString(&bkc.SecretKey, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.SecretKey }))

	m.SessionToken = envVarToString(bkc.SessionToken, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.SessionToken }))
	m.Region = envVarToString(bkc.Region, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.Region }))
	m.ARN = envVarToString(bkc.ARN, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.ARN }))
	m.RoleARN = envVarToString(bkc.RoleARN, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.RoleARN }))
	m.ExternalID = envVarToString(bkc.ExternalID, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.ExternalID }))
	m.RoleSessionName = envVarToString(bkc.RoleSessionName, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.RoleSessionName }))

	return m
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
