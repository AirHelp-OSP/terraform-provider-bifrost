package resources

import (
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

// TestApiKeyToProviderKeyModel_PreservesRedactedValue is the central
// "round-trip is idempotent" guard: after Create stores the plaintext value,
// every subsequent Read sees Bifrost's redacted form, and the projection must
// fall back to prior state to avoid a spurious diff.
func TestApiKeyToProviderKeyModel_PreservesRedactedValue(t *testing.T) {
	prior := &ProviderKeyResourceModel{
		Value: types.StringValue("sk-secret-from-config"),
	}
	apiKey := &schemas.Key{
		ID:   "uuid-1",
		Name: "primary",
		// Server-redacted form: 4-char prefix + 24 asterisks + 4-char suffix = 32 chars.
		// This is the exact shape schemas.EnvVar.IsRedacted recognizes.
		Value:  schemas.EnvVar{Val: "sk-s" + repeat("*", 24) + "tail"},
		Weight: 1.0,
	}

	got := apiKeyToProviderKeyModel(apiKey, prior, "bedrock")

	if got.Value.ValueString() != "sk-secret-from-config" {
		t.Errorf("Value: got %q, want preserved prior %q", got.Value.ValueString(), "sk-secret-from-config")
	}
	if got.KeyID.ValueString() != "uuid-1" {
		t.Errorf("KeyID: got %q, want %q", got.KeyID.ValueString(), "uuid-1")
	}
	if got.ID.ValueString() != "bedrock:primary" {
		t.Errorf("ID: got %q, want %q", got.ID.ValueString(), "bedrock:primary")
	}
}

// TestApiKeyToProviderKeyModel_PostImportRedactedYieldsNull simulates the
// first Read after ImportState: the only state seeded by the import handler
// is provider_name / name / key_id / id, so prior.Value is null. The server
// returns the redacted form. The projection must NOT store the redacted
// text in state — that would leak placeholder bytes into subsequent plans.
// Instead, state stays null so the next plan re-sends the user's HCL value.
func TestApiKeyToProviderKeyModel_PostImportRedactedYieldsNull(t *testing.T) {
	// prior mirrors what ImportState seeds: id, provider_name, name, key_id only.
	prior := &ProviderKeyResourceModel{
		ID:           types.StringValue("bedrock:primary"),
		ProviderName: types.StringValue("bedrock"),
		Name:         types.StringValue("primary"),
		KeyID:        types.StringValue("uuid-1"),
		Value:        types.StringNull(),
	}
	apiKey := &schemas.Key{
		ID:     "uuid-1",
		Name:   "primary",
		Value:  schemas.EnvVar{Val: "sk-s" + repeat("*", 24) + "tail"},
		Weight: 1.0,
	}
	got := apiKeyToProviderKeyModel(apiKey, prior, "bedrock")
	if !got.Value.IsNull() {
		t.Errorf("Value: got %q, want null (post-import redaction must not leak into state)", got.Value.ValueString())
	}
}

// TestApiKeyToProviderKeyModel_PassesThroughPlaintextValue ensures that when
// Bifrost does return a plaintext value (Create response on a fresh key), we
// store that value rather than the prior null/unknown.
func TestApiKeyToProviderKeyModel_PassesThroughPlaintextValue(t *testing.T) {
	apiKey := &schemas.Key{
		ID:     "uuid-2",
		Name:   "fresh",
		Value:  schemas.EnvVar{Val: "sk-fresh-plaintext"},
		Weight: 2.0,
	}
	got := apiKeyToProviderKeyModel(apiKey, nil, "openai")
	if got.Value.ValueString() != "sk-fresh-plaintext" {
		t.Errorf("Value: got %q, want plaintext %q", got.Value.ValueString(), "sk-fresh-plaintext")
	}
	if got.Weight.ValueFloat64() != 2.0 {
		t.Errorf("Weight: got %v, want 2.0", got.Weight.ValueFloat64())
	}
}

// TestApiKeyToProviderKeyModel_AliasesRoundTrip verifies the new
// `model_aliases` field (Bifrost v1.5.0 Key.Aliases — the canonical
// replacement for the Bedrock-only `deployments` map) projects into TF
// state without loss.
func TestApiKeyToProviderKeyModel_AliasesRoundTrip(t *testing.T) {
	apiKey := &schemas.Key{
		ID:     "uuid-3",
		Name:   "aliased",
		Value:  schemas.EnvVar{Val: "v"},
		Weight: 1.0,
		Aliases: schemas.KeyAliases{
			"claude-3-opus": "us.anthropic.claude-3-opus-20240229-v1:0",
			"gpt-4o-mini":   "deployment-mini",
		},
	}
	got := apiKeyToProviderKeyModel(apiKey, nil, "bedrock")
	if got.ModelAliases.IsNull() {
		t.Fatal("ModelAliases: got null, want populated map")
	}
	wantElems := map[string]string{
		"claude-3-opus": "us.anthropic.claude-3-opus-20240229-v1:0",
		"gpt-4o-mini":   "deployment-mini",
	}
	elems := got.ModelAliases.Elements()
	if len(elems) != len(wantElems) {
		t.Fatalf("ModelAliases length: got %d, want %d", len(elems), len(wantElems))
	}
	for k, want := range wantElems {
		ev, ok := elems[k]
		if !ok {
			t.Errorf("ModelAliases missing key %q", k)
			continue
		}
		gotStr, ok := ev.(types.String)
		if !ok {
			t.Errorf("ModelAliases[%q]: not a string", k)
			continue
		}
		if gotStr.ValueString() != want {
			t.Errorf("ModelAliases[%q]: got %q, want %q", k, gotStr.ValueString(), want)
		}
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
		Value:  schemas.EnvVar{Val: "v"},
		Weight: 1.0,
		Models: schemas.WhiteList{},
	}
	got := apiKeyToProviderKeyModel(apiKey, nil, "bedrock")
	if got.Models.IsNull() || got.Models.IsUnknown() {
		t.Fatalf("Models: got null/unknown, want empty list")
	}
	if elemCount := len(got.Models.Elements()); elemCount != 0 {
		t.Errorf("Models length: got %d, want 0 (deny-all)", elemCount)
	}

	// And confirm typed null path still works when server omits the field entirely.
	apiKey.Models = nil
	got = apiKeyToProviderKeyModel(apiKey, nil, "bedrock")
	if got.Models.IsNull() {
		t.Errorf("Models with nil server value: got null, want empty list value")
	}
}

// TestApiKeyToProviderKeyModel_OptionalValueRoundTrips covers the
// Bedrock-style use case where `value` is intentionally omitted because
// credentials live in bedrock_key_config. The user's HCL has no `value`,
// so plan resolves to null. On the round trip we send EnvVar{Val:""}; the
// API echoes the empty EnvVar back. Storing types.StringValue("") here
// would force a perpetual `null → ""` diff. envVarToString therefore
// folds empty values into the same null-preserving path as redacted ones.
func TestApiKeyToProviderKeyModel_OptionalValueRoundTrips(t *testing.T) {
	prior := &ProviderKeyResourceModel{
		Value: types.StringNull(),
	}
	apiKey := &schemas.Key{
		ID:    "uuid-bedrock",
		Name:  "sta",
		Value: schemas.EnvVar{Val: ""},
		BedrockKeyConfig: &schemas.BedrockKeyConfig{
			Region: schemas.NewEnvVar("eu-west-1"),
		},
		Weight: 1.0,
	}
	got := apiKeyToProviderKeyModel(apiKey, prior, "bedrock")
	if !got.Value.IsNull() {
		t.Errorf("Value: got %q, want null (empty API value with null prior must stay null to avoid '' → null diff)", got.Value.ValueString())
	}
}

// TestEnvVarToString_EmptyWithPriorPreservesPrior guards the symmetric case:
// when an EnvVar field round-trips as empty (e.g. transient server-side
// behavior on a sensitive field) but state already held a user-supplied
// plaintext, we must keep that plaintext rather than silently clearing it.
func TestEnvVarToString_EmptyWithPriorPreservesPrior(t *testing.T) {
	got := envVarToString(&schemas.EnvVar{Val: ""}, types.StringValue("AKIA-example"))
	if got.ValueString() != "AKIA-example" {
		t.Errorf("got %q, want preserved prior %q", got.ValueString(), "AKIA-example")
	}
}

// TestEnvVarToString_EmptyWithNullPriorYieldsNull is the critical assertion
// behind making `value` optional. envvar.go::IsRedacted returns false for
// empty + !FromEnv, so without explicit empty handling we'd return
// types.StringValue("") and trigger the spurious-diff loop.
func TestEnvVarToString_EmptyWithNullPriorYieldsNull(t *testing.T) {
	got := envVarToString(&schemas.EnvVar{Val: ""}, types.StringNull())
	if !got.IsNull() {
		t.Errorf("got %q, want null", got.ValueString())
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

// (unused import compiler guard — keeps `attr` available for future tests
// without churn if the file is later extended)
var _ = []attr.Value(nil)
