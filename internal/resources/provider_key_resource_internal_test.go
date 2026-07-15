package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/maximhq/bifrost/core/schemas"
)

// TestParseProviderKeyImportID covers the composite "<provider>:<key_name>"
// import format. Both halves must be non-empty; anything else is a user
// error and must surface a clear "Invalid import ID" diagnostic at import
// time (not later as a confusing "Key not found").
func TestParseProviderKeyImportID(t *testing.T) {
	cases := []struct {
		name         string
		in           string
		wantOK       bool
		wantProvider string
		wantKeyName  string
	}{
		{name: "happy", in: "bedrock:primary", wantOK: true, wantProvider: "bedrock", wantKeyName: "primary"},
		{name: "key name with colon-after-split-2", in: "openai:my:weird-key", wantOK: true, wantProvider: "openai", wantKeyName: "my:weird-key"},
		{name: "missing separator", in: "no-colon-here", wantOK: false},
		{name: "empty provider", in: ":primary", wantOK: false},
		{name: "empty key name", in: "bedrock:", wantOK: false},
		{name: "empty", in: "", wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotProvider, gotKey, ok := parseProviderKeyImportID(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if gotProvider != tc.wantProvider {
				t.Errorf("provider: got %q, want %q", gotProvider, tc.wantProvider)
			}
			if gotKey != tc.wantKeyName {
				t.Errorf("key name: got %q, want %q", gotKey, tc.wantKeyName)
			}
		})
	}
}

// TestApiKeyToProviderKeyModel_PreservesValueSHA256 is the central
// "round-trip is idempotent" guard: the secret is never stored, so Read must
// carry the prior value_sha256 digest forward (the API never returns it).
// Losing it would make every plan recompute a diff.
func TestApiKeyToProviderKeyModel_PreservesValueSHA256(t *testing.T) {
	prior := &ProviderKeyResourceModel{
		ValueSHA256: types.StringValue("9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"),
	}
	apiKey := &schemas.Key{
		ID:   "uuid-1",
		Name: "primary",
		// Server-redacted form: 4-char prefix + 24 asterisks + 4-char suffix = 32 chars.
		// This is the exact shape schemas.SecretVar.IsRedacted recognizes.
		Value:  schemas.SecretVar{Val: "sk-s" + repeat("*", 24) + "tail"},
		Weight: 1.0,
	}

	got := apiKeyToProviderKeyModel(context.Background(), apiKey, prior, "bedrock")

	if got.ValueSHA256.ValueString() != prior.ValueSHA256.ValueString() {
		t.Errorf("ValueSHA256: got %q, want preserved prior %q", got.ValueSHA256.ValueString(), prior.ValueSHA256.ValueString())
	}
	if !got.Value.IsNull() {
		t.Errorf("Value: got %q, want null (write-only must never be stored in state)", got.Value.ValueString())
	}
	if got.KeyID.ValueString() != "uuid-1" {
		t.Errorf("KeyID: got %q, want %q", got.KeyID.ValueString(), "uuid-1")
	}
	if got.ID.ValueString() != "bedrock:primary" {
		t.Errorf("ID: got %q, want %q", got.ID.ValueString(), "bedrock:primary")
	}
}

// TestApiKeyToProviderKeyModel_PostImportYieldsNullDigest simulates the first
// Read after ImportState: the import handler seeds only provider_name / name /
// key_id / id, so prior.ValueSHA256 is null. The projection must keep it null
// (a known digest would be a fabrication) so the next plan, which can hash the
// configured value_wo, drives the reconciling update.
func TestApiKeyToProviderKeyModel_PostImportYieldsNullDigest(t *testing.T) {
	prior := &ProviderKeyResourceModel{
		ID:           types.StringValue("bedrock:primary"),
		ProviderName: types.StringValue("bedrock"),
		Name:         types.StringValue("primary"),
		KeyID:        types.StringValue("uuid-1"),
		ValueSHA256:  types.StringNull(),
	}
	apiKey := &schemas.Key{
		ID:     "uuid-1",
		Name:   "primary",
		Value:  schemas.SecretVar{Val: "sk-s" + repeat("*", 24) + "tail"},
		Weight: 1.0,
	}
	got := apiKeyToProviderKeyModel(context.Background(), apiKey, prior, "bedrock")
	if !got.ValueSHA256.IsNull() {
		t.Errorf("ValueSHA256: got %q, want null (import seeds no digest)", got.ValueSHA256.ValueString())
	}
}

// TestApiKeyToProviderKeyModel_NilPriorYieldsNullDigest ensures a projection
// with no prior (an unusual path now that Create overwrites the digest itself)
// produces a null digest rather than panicking on a nil prior.
func TestApiKeyToProviderKeyModel_NilPriorYieldsNullDigest(t *testing.T) {
	apiKey := &schemas.Key{
		ID:     "uuid-2",
		Name:   "fresh",
		Value:  schemas.SecretVar{Val: "sk-fresh-plaintext"},
		Weight: 2.0,
	}
	got := apiKeyToProviderKeyModel(context.Background(), apiKey, nil, "openai")
	if !got.ValueSHA256.IsNull() {
		t.Errorf("ValueSHA256: got %q, want null for nil prior", got.ValueSHA256.ValueString())
	}
	if !got.Value.IsNull() {
		t.Errorf("Value: got %q, want null", got.Value.ValueString())
	}
	if got.Weight.ValueFloat64() != 2.0 {
		t.Errorf("Weight: got %v, want 2.0", got.Weight.ValueFloat64())
	}
}

// TestApiKeyToProviderKeyModel_AliasesRoundTrip verifies the rich `model_aliases`
// map (Bifrost v1.6.x Key.Aliases / AliasConfig) projects into nested-object TF
// state without loss: a simple system-profile alias (only model_id) keeps its
// optional fields null, while an application-profile alias round-trips its
// inference_profile_arn override plus model_name/model_family routing metadata.
func TestApiKeyToProviderKeyModel_AliasesRoundTrip(t *testing.T) {
	const arn = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc"
	family := schemas.ModelFamilyAnthropic
	modelName := "claude-3-5-sonnet-20241022"

	apiKey := &schemas.Key{
		ID:     "uuid-3",
		Name:   "aliased",
		Value:  schemas.SecretVar{Val: "v"},
		Weight: 1.0,
		Aliases: schemas.KeyAliases{
			// simple entry: cross-region system inference profile (only model_id)
			"claude-system": {ModelID: "us.anthropic.claude-3-5-sonnet-20241022-v2:0"},
			// rich entry: application inference profile ARN + routing metadata
			"claude-app": {
				ModelID:     "anthropic.claude-3-5-sonnet-20241022-v2:0",
				ModelName:   &modelName,
				ModelFamily: &family,
				Description: "prod Claude on our AWS tenant",
				BedrockAliasCfg: &schemas.BedrockAliasCfg{
					InferenceProfileARN: schemas.NewSecretVar(arn),
				},
			},
		},
	}

	got := apiKeyToProviderKeyModel(context.Background(), apiKey, nil, "bedrock")
	if got.ModelAliases.IsNull() {
		t.Fatal("ModelAliases: got null, want populated map")
	}
	aliases := map[string]AliasConfigModel{}
	if diags := got.ModelAliases.ElementsAs(context.Background(), &aliases, false); diags.HasError() {
		t.Fatalf("ElementsAs: %v", diags)
	}
	if len(aliases) != 2 {
		t.Fatalf("alias count: got %d, want 2", len(aliases))
	}

	sys, ok := aliases["claude-system"]
	if !ok {
		t.Fatal("missing alias claude-system")
	}
	if sys.ModelID.ValueString() != "us.anthropic.claude-3-5-sonnet-20241022-v2:0" {
		t.Errorf("claude-system model_id: got %q", sys.ModelID.ValueString())
	}
	if !sys.InferenceProfileARN.IsNull() || !sys.ModelName.IsNull() ||
		!sys.ModelFamily.IsNull() || !sys.Description.IsNull() || !sys.Region.IsNull() {
		t.Errorf("claude-system: unset optional fields should be null, got %+v", sys)
	}

	app, ok := aliases["claude-app"]
	if !ok {
		t.Fatal("missing alias claude-app")
	}
	if app.ModelID.ValueString() != "anthropic.claude-3-5-sonnet-20241022-v2:0" {
		t.Errorf("claude-app model_id: got %q", app.ModelID.ValueString())
	}
	if app.InferenceProfileARN.ValueString() != arn {
		t.Errorf("claude-app inference_profile_arn: got %q, want %q", app.InferenceProfileARN.ValueString(), arn)
	}
	if app.ModelName.ValueString() != modelName {
		t.Errorf("claude-app model_name: got %q, want %q", app.ModelName.ValueString(), modelName)
	}
	if app.ModelFamily.ValueString() != string(family) {
		t.Errorf("claude-app model_family: got %q, want %q", app.ModelFamily.ValueString(), string(family))
	}
	if app.Description.ValueString() != "prod Claude on our AWS tenant" {
		t.Errorf("claude-app description: got %q", app.Description.ValueString())
	}
}

// TestModelAliasesToAPI_RichRoundTrip verifies the plan-model → schemas.KeyAliases
// direction: every nested field maps to its AliasConfig counterpart, the Bedrock
// inference_profile_arn lands under BedrockAliasCfg, and an unset field is omitted.
func TestModelAliasesToAPI_RichRoundTrip(t *testing.T) {
	const arn = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc"
	m := types.MapValueMust(aliasConfigObjectType(), map[string]attr.Value{
		"claude-app": types.ObjectValueMust(aliasConfigAttrTypes(), map[string]attr.Value{
			"model_id":              types.StringValue("anthropic.claude-3-5-sonnet-20241022-v2:0"),
			"inference_profile_arn": types.StringValue(arn),
			"model_name":            types.StringValue("claude-3-5-sonnet-20241022"),
			"model_family":          types.StringValue("anthropic"),
			"description":           types.StringNull(),
			"region":                types.StringValue("us-east-1"),
		}),
	})

	ka, diags := modelAliasesToAPI(context.Background(), m)
	if diags.HasError() {
		t.Fatalf("modelAliasesToAPI: %v", diags)
	}
	ac, ok := ka["claude-app"]
	if !ok {
		t.Fatal("missing alias claude-app")
	}
	if ac.ModelID != "anthropic.claude-3-5-sonnet-20241022-v2:0" {
		t.Errorf("ModelID: got %q", ac.ModelID)
	}
	if ac.ModelName == nil || *ac.ModelName != "claude-3-5-sonnet-20241022" {
		t.Errorf("ModelName: got %v", ac.ModelName)
	}
	if ac.ModelFamily == nil || *ac.ModelFamily != schemas.ModelFamilyAnthropic {
		t.Errorf("ModelFamily: got %v", ac.ModelFamily)
	}
	if ac.Description != "" {
		t.Errorf("Description: got %q, want empty (unset)", ac.Description)
	}
	if ac.Region == nil || ac.Region.GetValue() != "us-east-1" {
		t.Errorf("Region: got %v", ac.Region)
	}
	if ac.BedrockAliasCfg == nil || ac.InferenceProfileARN == nil ||
		ac.InferenceProfileARN.GetValue() != arn {
		t.Errorf("InferenceProfileARN: got %v", ac.BedrockAliasCfg)
	}
}

// TestModelAliasesToAPI_NullYieldsNil ensures an unset map sends no aliases.
func TestModelAliasesToAPI_NullYieldsNil(t *testing.T) {
	ka, diags := modelAliasesToAPI(context.Background(), types.MapNull(aliasConfigObjectType()))
	if diags.HasError() {
		t.Fatalf("modelAliasesToAPI: %v", diags)
	}
	if ka != nil {
		t.Errorf("KeyAliases: got %v, want nil", ka)
	}
}

// TestApiAliasesToModel_EmptyYieldsNull ensures empty/nil server aliases project
// to a typed null map so an Optional+unset attribute round-trips cleanly.
func TestApiAliasesToModel_EmptyYieldsNull(t *testing.T) {
	priorNull := types.MapNull(aliasConfigObjectType())
	if got := apiAliasesToModel(context.Background(), schemas.KeyAliases{}, priorNull); !got.IsNull() {
		t.Errorf("empty aliases: got %v, want null", got)
	}
	if got := apiAliasesToModel(context.Background(), nil, priorNull); !got.IsNull() {
		t.Errorf("nil aliases: got %v, want null", got)
	}
}

// TestApiKeyToProviderKeyModel_EmptyModelsListNotSubstituted ensures we
// faithfully project an empty Key.Models from the server back into state.
// Under Bifrost v1.5.0 BC1, `[]` means deny-all and must NOT be silently
// rewritten to `["*"]` on Read — that would mask a real server state.
func TestApiKeyToProviderKeyModel_EmptyModelsListNotSubstituted(t *testing.T) {
	apiKey := &schemas.Key{
		ID:     "uuid-4",
		Name:   "denyall",
		Value:  schemas.SecretVar{Val: "v"},
		Weight: 1.0,
		Models: schemas.WhiteList{},
	}
	got := apiKeyToProviderKeyModel(context.Background(), apiKey, nil, "bedrock")
	if got.Models.IsNull() || got.Models.IsUnknown() {
		t.Fatalf("Models: got null/unknown, want empty list")
	}
	if elemCount := len(got.Models.Elements()); elemCount != 0 {
		t.Errorf("Models length: got %d, want 0 (deny-all)", elemCount)
	}

	// And confirm typed null path still works when server omits the field entirely.
	apiKey.Models = nil
	got = apiKeyToProviderKeyModel(context.Background(), apiKey, nil, "bedrock")
	if got.Models.IsNull() {
		t.Errorf("Models with nil server value: got null, want empty list value")
	}
}

// TestApiKeyToProviderKeyModel_OptionalValueRoundTrips covers the
// Bedrock-style use case where `value_wo` is intentionally omitted because
// credentials live in bedrock_key_config. With no secret configured, the
// prior digest is null and must stay null across the round trip — a non-null
// digest would force a perpetual diff on a key that has no value.
func TestApiKeyToProviderKeyModel_OptionalValueRoundTrips(t *testing.T) {
	prior := &ProviderKeyResourceModel{
		ValueSHA256: types.StringNull(),
	}
	apiKey := &schemas.Key{
		ID:    "uuid-bedrock",
		Name:  "sta",
		Value: schemas.SecretVar{Val: ""},
		BedrockKeyConfig: &schemas.BedrockKeyConfig{
			Region: schemas.NewSecretVar("eu-west-1"),
		},
		Weight: 1.0,
	}
	got := apiKeyToProviderKeyModel(context.Background(), apiKey, prior, "bedrock")
	if !got.ValueSHA256.IsNull() {
		t.Errorf("ValueSHA256: got %q, want null (no value_wo configured)", got.ValueSHA256.ValueString())
	}
	if got.BedrockKeyConfig == nil {
		t.Fatal("BedrockKeyConfig: got nil, want populated from API response")
	}
}

// TestEnvVarToString_EmptyWithPriorPreservesPrior guards the symmetric case:
// when an EnvVar field round-trips as empty (e.g. transient server-side
// behavior on a sensitive field) but state already held a user-supplied
// plaintext, we must keep that plaintext rather than silently clearing it.
func TestEnvVarToString_EmptyWithPriorPreservesPrior(t *testing.T) {
	got := envVarToString(&schemas.SecretVar{Val: ""}, types.StringValue("AKIA-example"))
	if got.ValueString() != "AKIA-example" {
		t.Errorf("got %q, want preserved prior %q", got.ValueString(), "AKIA-example")
	}
}

// TestEnvVarToString_EmptyWithNullPriorYieldsNull is the critical assertion
// behind making `value` optional. envvar.go::IsRedacted returns false for
// empty + !FromEnv, so without explicit empty handling we'd return
// types.StringValue("") and trigger the spurious-diff loop.
func TestEnvVarToString_EmptyWithNullPriorYieldsNull(t *testing.T) {
	got := envVarToString(&schemas.SecretVar{Val: ""}, types.StringNull())
	if !got.IsNull() {
		t.Errorf("got %q, want null", got.ValueString())
	}
}

// TestEnvVarToString_NonRedactedServerValueDoesNotOverridePrior is the
// regression guard for the cross-version "inconsistent values for sensitive
// attribute" apply failure. Bifrost redacts these fields on read, so a
// non-redacted API echo is never a source of truth. When the response carries a
// concrete value that differs from the known prior — the plan during
// Create/Update, existing state during Read — the projection must keep the prior
// so applied state equals the plan. Two shapes trigger it in the wild:
//   - a legacy JSON secret blob written by a pre-v1.6 provider (core v1.5.x),
//     which could not parse the v1.6 SecretVar wire form and stored the raw
//     `{"value":"...","type":"plain_text"}` string as the field value; and
//   - any server that returns a stored value non-redacted.
//
// Either one, passed through verbatim, diverges from the plan and aborts apply.
func TestEnvVarToString_NonRedactedServerValueDoesNotOverridePrior(t *testing.T) {
	// Plan omits the credential (null); server echoes a legacy blob. Must be null.
	blob := `{"value":"AKIA` + repeat("*", 24) + `MPLE","type":"plain_text"}`
	if got := envVarToString(&schemas.SecretVar{Val: blob}, types.StringNull()); !got.IsNull() {
		t.Errorf("legacy blob + null prior: got %q, want null", got.ValueString())
	}
	// Plan sets region; server returns a different concrete value. Must keep plan.
	if got := envVarToString(&schemas.SecretVar{Val: "us-east-1"}, types.StringValue("eu-west-1")); got.ValueString() != "eu-west-1" {
		t.Errorf("server drift + prior: got %q, want plan value %q", got.ValueString(), "eu-west-1")
	}
	// No prior (import / first read): a genuine plaintext is still adopted.
	if got := envVarToString(&schemas.SecretVar{Val: "us-east-1"}, types.StringNull()); got.ValueString() != "us-east-1" {
		t.Errorf("plaintext + null prior: got %q, want adopted %q", got.ValueString(), "us-east-1")
	}
}

// TestEnvVarToString_UnknownPriorResolvesFromAPI guards the Computed-attribute
// path (e.g. network_config.ca_cert_pem, which plans as unknown): every value
// must be known after apply, so an unknown prior must be resolved from the API
// response — never returned verbatim, which errors as "unknown value after
// apply". This is the regression the prior-first reordering introduced.
func TestEnvVarToString_UnknownPriorResolvesFromAPI(t *testing.T) {
	// unknown prior + nil/placeholder server value → known null
	if got := envVarToString(nil, types.StringUnknown()); !got.IsNull() {
		t.Errorf("unknown prior + nil server: got %q, want null", got.ValueString())
	}
	// unknown prior + genuine server value → adopt it (known)
	if got := envVarToString(schemas.NewSecretVar("pem-data"), types.StringUnknown()); got.ValueString() != "pem-data" {
		t.Errorf("unknown prior + server value: got %q, want adopted %q", got.ValueString(), "pem-data")
	}
}

// TestBedrockKeyConfigToModel_UpgradeFromLegacyBlobStateIsConsistent mirrors the
// full upgrade path a Bedrock user hits after moving off a pre-v1.6 provider:
// prior state holds JSON-blob-poisoned credentials, the config now omits the
// creds and keeps a plaintext region, and the partial update makes Bifrost clear
// the creds ({"value":""}) and return the region redacted. The projected state
// must equal the plan (null creds, plaintext region) — anything else is the
// "inconsistent values for sensitive attribute" error.
func TestBedrockKeyConfigToModel_UpgradeFromLegacyBlobStateIsConsistent(t *testing.T) {
	api := &schemas.BedrockKeyConfig{
		AccessKey: schemas.SecretVar{Val: ""},                              // server cleared
		SecretKey: schemas.SecretVar{Val: ""},                              // server cleared
		Region:    schemas.NewSecretVar("eu-w" + repeat("*", 24) + "st-1"), // redacted
	}
	plan := &BedrockKeyConfigModel{
		AccessKey: types.StringNull(),
		SecretKey: types.StringNull(),
		Region:    types.StringValue("eu-west-1"),
	}
	got := bedrockKeyConfigToModel(api, plan)
	if !got.AccessKey.IsNull() {
		t.Errorf("access_key: got %q, want null (must match planned null)", got.AccessKey.ValueString())
	}
	if !got.SecretKey.IsNull() {
		t.Errorf("secret_key: got %q, want null (must match planned null)", got.SecretKey.ValueString())
	}
	if got.Region.ValueString() != "eu-west-1" {
		t.Errorf("region: got %q, want planned %q", got.Region.ValueString(), "eu-west-1")
	}
}

// TestBedrockKeyConfigToModel_RawServerCredsDoNotOverridePlan reproduces the
// real-world upgrade failure: some Bifrost deployments echo the stored
// credentials back NON-redacted on the update response (unlike stock v1.6.4,
// which returns them empty). The config omits the creds, so the plan is null for
// both — and the applied state must be null too. If the projection adopts the
// non-redacted echo instead, applied state diverges from the plan and Terraform
// aborts with ".bedrock_key_config: inconsistent values for sensitive attribute".
func TestBedrockKeyConfigToModel_RawServerCredsDoNotOverridePlan(t *testing.T) {
	api := &schemas.BedrockKeyConfig{
		AccessKey: *schemas.NewSecretVar("AKIAIOSFODNN7EXAMPLE"),                     // raw, non-redacted
		SecretKey: *schemas.NewSecretVar("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"), // raw, non-redacted
		Region:    schemas.NewSecretVar("eu-west-1"),                                 // raw, non-redacted
	}
	plan := &BedrockKeyConfigModel{ // config omits the creds; keeps region
		AccessKey: types.StringNull(),
		SecretKey: types.StringNull(),
		Region:    types.StringValue("eu-west-1"),
	}
	got := bedrockKeyConfigToModel(api, plan)
	if !got.AccessKey.IsNull() {
		t.Errorf("access_key: got %q, want null (must match planned null)", got.AccessKey.ValueString())
	}
	if !got.SecretKey.IsNull() {
		t.Errorf("secret_key: got %q, want null (must match planned null)", got.SecretKey.ValueString())
	}
	if got.Region.ValueString() != "eu-west-1" {
		t.Errorf("region: got %q, want planned %q", got.Region.ValueString(), "eu-west-1")
	}
}

// TestBedrockKeyConfigToModel_ImportProjectsFromServer confirms the no-prior
// path still projects from the API (folding redacted/empty/blob placeholders to
// null) so the first Read after ImportState is populated rather than empty.
func TestBedrockKeyConfigToModel_ImportProjectsFromServer(t *testing.T) {
	api := &schemas.BedrockKeyConfig{
		AccessKey: schemas.SecretVar{Val: "AKIA" + repeat("*", 24) + "MPLE"}, // redacted → null
		Region:    schemas.NewSecretVar("us-east-1"),                         // plaintext → adopted
	}
	got := bedrockKeyConfigToModel(api, nil)
	if !got.AccessKey.IsNull() {
		t.Errorf("access_key: got %q, want null (redacted on import)", got.AccessKey.ValueString())
	}
	if got.Region.ValueString() != "us-east-1" {
		t.Errorf("region: got %q, want adopted %q", got.Region.ValueString(), "us-east-1")
	}
}

// sha256Hello is the well-known external digest SHA-256("hello"), used to
// guard sha256OfValue against accidentally hashing the wrong bytes or
// double-encoding.
const sha256Hello = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

// TestSHA256OfValue covers the digest helper that powers both change detection
// and the v0→v1 migration: a real value hashes to its hex digest, while
// null/empty/unknown all fold to null so an unset secret never round-trips as
// the hash of an empty string.
func TestSHA256OfValue(t *testing.T) {
	if got := sha256OfValue(types.StringValue("hello")); got.ValueString() != sha256Hello {
		t.Errorf("hash(hello): got %q, want %q", got.ValueString(), sha256Hello)
	}
	if got := sha256OfValue(types.StringNull()); !got.IsNull() {
		t.Errorf("hash(null): got %q, want null", got.ValueString())
	}
	if got := sha256OfValue(types.StringValue("")); !got.IsNull() {
		t.Errorf("hash(empty): got %q, want null", got.ValueString())
	}
	if got := sha256OfValue(types.StringUnknown()); !got.IsNull() {
		t.Errorf("hash(unknown): got %q, want null", got.ValueString())
	}
}

// TestUpgradeProviderKeyModelV0toV2 verifies the v0 -> current migration: the
// plaintext `value` is replaced by its digest and left null (it is now
// write-only), and every other field is carried over.
func TestUpgradeProviderKeyModelV0toV2(t *testing.T) {
	old := providerKeyResourceModelV0{
		ID:           types.StringValue("openai:primary"),
		ProviderName: types.StringValue("openai"),
		Name:         types.StringValue("primary"),
		KeyID:        types.StringValue("uuid-9"),
		Value:        types.StringValue("hello"),
		Weight:       types.Float64Value(2.0),
		Enabled:      types.BoolValue(true),
	}

	got, diags := upgradeProviderKeyModelV0toV2(context.Background(), old)
	if diags.HasError() {
		t.Fatalf("upgrade: %v", diags)
	}

	if got.ValueSHA256.ValueString() != sha256Hello {
		t.Errorf("ValueSHA256: got %q, want %q", got.ValueSHA256.ValueString(), sha256Hello)
	}
	if !got.Value.IsNull() {
		t.Errorf("Value: got %q, want null after upgrade", got.Value.ValueString())
	}
	if got.KeyID.ValueString() != "uuid-9" {
		t.Errorf("KeyID: got %q, want carried-over %q", got.KeyID.ValueString(), "uuid-9")
	}
	if got.Weight.ValueFloat64() != 2.0 {
		t.Errorf("Weight: got %v, want carried-over 2.0", got.Weight.ValueFloat64())
	}
}

// TestUpgradeProviderKeyModelV0toV2_NoValue covers a Bedrock-style key that had
// no plaintext value in v0: it must upgrade to a null digest, not the hash of
// an empty string.
func TestUpgradeProviderKeyModelV0toV2_NoValue(t *testing.T) {
	old := providerKeyResourceModelV0{
		Name:  types.StringValue("bedrock-key"),
		Value: types.StringNull(),
	}
	got, diags := upgradeProviderKeyModelV0toV2(context.Background(), old)
	if diags.HasError() {
		t.Fatalf("upgrade: %v", diags)
	}
	if !got.ValueSHA256.IsNull() {
		t.Errorf("ValueSHA256: got %q, want null for a key with no value", got.ValueSHA256.ValueString())
	}
}

// TestUpgradeProviderKeyModelV1toV2 verifies the alias migration: a v1 string
// alias map ({name = model_id}) becomes the v2 nested-object form with only
// model_id set; the digest and other fields carry over unchanged.
func TestUpgradeProviderKeyModelV1toV2(t *testing.T) {
	old := providerKeyResourceModelV1{
		ID:          types.StringValue("bedrock:primary"),
		Name:        types.StringValue("primary"),
		KeyID:       types.StringValue("uuid-7"),
		ValueSHA256: types.StringValue(sha256Hello),
		Weight:      types.Float64Value(1.0),
		ModelAliases: types.MapValueMust(types.StringType, map[string]attr.Value{
			"claude": types.StringValue("us.anthropic.claude-3-opus-20240229-v1:0"),
		}),
	}

	got, diags := upgradeProviderKeyModelV1toV2(context.Background(), old)
	if diags.HasError() {
		t.Fatalf("upgrade: %v", diags)
	}
	if got.ValueSHA256.ValueString() != sha256Hello {
		t.Errorf("ValueSHA256: got %q, want carried-over digest", got.ValueSHA256.ValueString())
	}
	if got.ModelAliases.IsNull() {
		t.Fatal("ModelAliases: got null, want migrated map")
	}
	aliases := map[string]AliasConfigModel{}
	if d := got.ModelAliases.ElementsAs(context.Background(), &aliases, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	claude, ok := aliases["claude"]
	if !ok {
		t.Fatal("missing migrated alias claude")
	}
	if claude.ModelID.ValueString() != "us.anthropic.claude-3-opus-20240229-v1:0" {
		t.Errorf("model_id: got %q", claude.ModelID.ValueString())
	}
	if !claude.InferenceProfileARN.IsNull() || !claude.ModelName.IsNull() ||
		!claude.ModelFamily.IsNull() || !claude.Region.IsNull() {
		t.Errorf("migrated alias optional fields should be null, got %+v", claude)
	}
}

// TestUpgradeProviderKeyModelV1toV2_NullAliases ensures an unset v1 alias map
// upgrades to a typed null rather than an empty populated map.
func TestUpgradeProviderKeyModelV1toV2_NullAliases(t *testing.T) {
	old := providerKeyResourceModelV1{
		Name:         types.StringValue("no-aliases"),
		ModelAliases: types.MapNull(types.StringType),
	}
	got, diags := upgradeProviderKeyModelV1toV2(context.Background(), old)
	if diags.HasError() {
		t.Fatalf("upgrade: %v", diags)
	}
	if !got.ModelAliases.IsNull() {
		t.Errorf("ModelAliases: got %v, want null", got.ModelAliases)
	}
}

// helpers ----------------------------------------------------------------

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
