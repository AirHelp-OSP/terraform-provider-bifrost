package resources

import (
	"context"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"

	bifrostclient "github.com/airhelp-osp/terraform-provider-bifrost/internal/client"
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

// TestModelToUpdateRequest_BlocksAlwaysSent guards the Bifrost v2.x update
// contract: concurrency_and_buffer_size is required on every PUT and
// network_config is replaced wholesale, so unmanaged blocks must echo the
// server's values and removed blocks must reset to the schema defaults.
func TestModelToUpdateRequest_BlocksAlwaysSent(t *testing.T) {
	current := &bifrostclient.ProviderResponse{
		NetworkConfig: schemas.NetworkConfig{
			BaseURL:                        "https://api.cloudflare.com/client/v4",
			DefaultRequestTimeoutInSeconds: 60,
			AllowPrivateNetwork:            true,
		},
		ConcurrencyAndBufferSize: schemas.ConcurrencyAndBufferSize{Concurrency: 10, BufferSize: 20},
	}

	unmanaged, diags := modelToUpdateRequest(context.Background(), ProviderResourceModel{}, ProviderResourceModel{}, current)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if unmanaged.NetworkConfig.BaseURL != current.NetworkConfig.BaseURL || unmanaged.ConcurrencyAndBufferSize.Concurrency != 10 {
		t.Errorf("unmanaged blocks must echo server values, got %+v / %+v", *unmanaged.NetworkConfig, *unmanaged.ConcurrencyAndBufferSize)
	}
	if unmanaged.ProxyConfig != nil {
		t.Errorf("unmanaged proxy_config must not be sent, got %+v", *unmanaged.ProxyConfig)
	}

	prior := ProviderResourceModel{
		NetworkConfig:            &NetworkConfigModel{},
		ConcurrencyAndBufferSize: &ConcurrencyBufferSizeModel{},
		ProxyConfig:              &ProxyConfigModel{},
	}
	removed, diags := modelToUpdateRequest(context.Background(), ProviderResourceModel{}, prior, current)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	nc := removed.NetworkConfig
	if nc.BaseURL != "" || nc.DefaultRequestTimeoutInSeconds != defaultRequestTimeoutSeconds {
		t.Errorf("removed network_config must reset managed fields, got %+v", *nc)
	}
	if !nc.AllowPrivateNetwork {
		t.Error("removed network_config must keep settings this resource does not manage")
	}
	if removed.ConcurrencyAndBufferSize.Concurrency != defaultConcurrency {
		t.Errorf("removed concurrency_and_buffer_size must reset to defaults, got %+v", *removed.ConcurrencyAndBufferSize)
	}
	if removed.ProxyConfig == nil || removed.ProxyConfig.Type != schemas.NoProxy {
		t.Errorf("removed proxy_config must be sent as type none, got %+v", removed.ProxyConfig)
	}
}
