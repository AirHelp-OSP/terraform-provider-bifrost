package resources

import (
	"context"

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
func envVarToString(ev *schemas.EnvVar, prior types.String) types.String {
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
func stringToEnvVar(s types.String) *schemas.EnvVar {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	return schemas.NewEnvVar(s.ValueString())
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

func bedrockKeyConfigToModel(bkc *schemas.BedrockKeyConfig, prior *BedrockKeyConfigModel) (*BedrockKeyConfigModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	m := &BedrockKeyConfigModel{}

	priorOf := func(get func(*BedrockKeyConfigModel) types.String) types.String {
		if prior == nil {
			return types.StringNull()
		}
		return get(prior)
	}

	// AccessKey / SecretKey are value types (EnvVar, not *EnvVar) in v1.5.0.
	m.AccessKey = envVarToString(&bkc.AccessKey, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.AccessKey }))
	m.SecretKey = envVarToString(&bkc.SecretKey, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.SecretKey }))

	m.SessionToken = envVarToString(bkc.SessionToken, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.SessionToken }))
	m.Region = envVarToString(bkc.Region, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.Region }))
	m.ARN = envVarToString(bkc.ARN, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.ARN }))
	m.RoleARN = envVarToString(bkc.RoleARN, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.RoleARN }))
	m.ExternalID = envVarToString(bkc.ExternalID, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.ExternalID }))
	m.RoleSessionName = envVarToString(bkc.RoleSessionName, priorOf(func(p *BedrockKeyConfigModel) types.String { return p.RoleSessionName }))

	return m, diags
}

func modelToBedrockKeyConfig(_ context.Context, m *BedrockKeyConfigModel) (*schemas.BedrockKeyConfig, diag.Diagnostics) {
	var diags diag.Diagnostics
	bkc := &schemas.BedrockKeyConfig{}

	if !m.AccessKey.IsNull() && !m.AccessKey.IsUnknown() {
		bkc.AccessKey = *schemas.NewEnvVar(m.AccessKey.ValueString())
	}
	if !m.SecretKey.IsNull() && !m.SecretKey.IsUnknown() {
		bkc.SecretKey = *schemas.NewEnvVar(m.SecretKey.ValueString())
	}
	bkc.SessionToken = stringToEnvVar(m.SessionToken)
	bkc.Region = stringToEnvVar(m.Region)
	bkc.ARN = stringToEnvVar(m.ARN)
	bkc.RoleARN = stringToEnvVar(m.RoleARN)
	bkc.ExternalID = stringToEnvVar(m.ExternalID)
	bkc.RoleSessionName = stringToEnvVar(m.RoleSessionName)

	return bkc, diags
}
