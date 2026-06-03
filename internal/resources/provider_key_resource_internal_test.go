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
		// This is the exact shape schemas.EnvVar.IsRedacted recognizes.
		Value:  schemas.EnvVar{Val: "sk-s" + repeat("*", 24) + "tail"},
		Weight: 1.0,
	}

	got := apiKeyToProviderKeyModel(apiKey, prior, "bedrock")

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
		Value:  schemas.EnvVar{Val: "sk-s" + repeat("*", 24) + "tail"},
		Weight: 1.0,
	}
	got := apiKeyToProviderKeyModel(apiKey, prior, "bedrock")
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
		Value:  schemas.EnvVar{Val: "sk-fresh-plaintext"},
		Weight: 2.0,
	}
	got := apiKeyToProviderKeyModel(apiKey, nil, "openai")
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
		Value: schemas.EnvVar{Val: ""},
		BedrockKeyConfig: &schemas.BedrockKeyConfig{
			Region: schemas.NewEnvVar("eu-west-1"),
		},
		Weight: 1.0,
	}
	got := apiKeyToProviderKeyModel(apiKey, prior, "bedrock")
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

// TestUpgradeProviderKeyModelV0toV1 verifies the state migration: the plaintext
// `value` is replaced by its digest and left null (it is now write-only, so it
// is never stored), and every other field is carried over.
func TestUpgradeProviderKeyModelV0toV1(t *testing.T) {
	old := providerKeyResourceModelV0{
		ID:           types.StringValue("openai:primary"),
		ProviderName: types.StringValue("openai"),
		Name:         types.StringValue("primary"),
		KeyID:        types.StringValue("uuid-9"),
		Value:        types.StringValue("hello"),
		Weight:       types.Float64Value(2.0),
		Enabled:      types.BoolValue(true),
	}

	got := upgradeProviderKeyModelV0toV1(old)

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

// TestUpgradeProviderKeyModelV0toV1_NoValue covers a Bedrock-style key that had
// no plaintext value in v0: it must upgrade to a null digest, not the hash of
// an empty string.
func TestUpgradeProviderKeyModelV0toV1_NoValue(t *testing.T) {
	old := providerKeyResourceModelV0{
		Name:  types.StringValue("bedrock-key"),
		Value: types.StringNull(),
	}
	if got := upgradeProviderKeyModelV0toV1(old); !got.ValueSHA256.IsNull() {
		t.Errorf("ValueSHA256: got %q, want null for a key with no value", got.ValueSHA256.ValueString())
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
