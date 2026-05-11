package resources

import (
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

// TestNetworkConfigToModel_EmptyBaseURLBecomesNull guards the regression
// behind the "inconsistent values for sensitive attribute" import-time
// error. Bifrost returns "" for an unconfigured BaseURL; wrapping that as
// types.StringValue("") puts the post-import state at odds with a plan
// that resolved the omitted attribute to null. The first apply then
// fails with the framework's terse "inconsistent result" diagnostic,
// obscured by network_config containing the sensitive ca_cert_pem
// sibling.
func TestNetworkConfigToModel_EmptyBaseURLBecomesNull(t *testing.T) {
	nc := schemas.NetworkConfig{
		BaseURL:                        "",
		DefaultRequestTimeoutInSeconds: 60,
		MaxRetries:                     3,
	}
	got := networkConfigToModel(nc, nil)
	if got.diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", got.diags)
	}
	if !got.model.BaseURL.IsNull() {
		t.Errorf("BaseURL: got %q, want null (empty API value must not poison state)", got.model.BaseURL.ValueString())
	}
}

// TestNetworkConfigToModel_PopulatedBaseURLPassesThrough confirms the
// non-empty path still stores the API value verbatim — the fix must not
// turn a real BaseURL into null.
func TestNetworkConfigToModel_PopulatedBaseURLPassesThrough(t *testing.T) {
	nc := schemas.NetworkConfig{
		BaseURL: "https://api.openai.com/v1",
	}
	got := networkConfigToModel(nc, nil)
	if got.diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", got.diags)
	}
	if got.model.BaseURL.ValueString() != "https://api.openai.com/v1" {
		t.Errorf("BaseURL: got %q, want %q", got.model.BaseURL.ValueString(), "https://api.openai.com/v1")
	}
}
