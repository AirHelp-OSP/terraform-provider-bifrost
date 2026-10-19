package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"
)

// TestBifrostProviderCloudflareReconfigure walks a Cloudflare provider through
// every kind of change: reconfiguring each block, removing them all, updating
// with no blocks at all (Bifrost requires concurrency_and_buffer_size on every
// PUT and replaces network_config wholesale), out-of-band deletion, an invalid
// key change, and destroy.
func TestBifrostProviderCloudflareReconfigure(t *testing.T) {
	tfDir := test_structure.CopyTerraformFolderToTemp(t, ".", "testdata/provider_cloudflare")

	opts := &terraform.Options{
		TerraformDir:             tfDir,
		TerraformBinary:          "tofu",
		RetryableTerraformErrors: bifrostRetryable,
		MaxRetries:               3,
		TimeBetweenRetries:       2 * time.Second,
		EnvVars: map[string]string{
			"TF_CLI_CONFIG_FILE": tofuRCPath,
		},
		Vars: map[string]interface{}{
			"endpoint":   bifrostEndpoint,
			"username":   bifrostUsername,
			"password":   bifrostPassword,
			"account_id": "0123456789abcdef0123456789abcdef",
			"network_config": map[string]interface{}{
				"base_url":                           "https://api.cloudflare.com/client/v4",
				"default_request_timeout_in_seconds": 60,
				"max_retries":                        2,
			},
			"concurrency_and_buffer_size": map[string]interface{}{"concurrency": 10, "buffer_size": 20},
			"proxy_config":                map[string]interface{}{"type": "http", "url": "http://proxy-a.example.com:3128"},
		},
	}
	defer terraform.Destroy(t, opts)

	applyClean := func(step string) {
		t.Helper()
		terraform.Apply(t, opts)
		if exitCode := terraform.PlanExitCode(t, opts); exitCode != 0 {
			t.Errorf("%s: expected no changes after apply (exit 0), got exit %d", step, exitCode)
		}
	}

	applyClean("create")
	p := getProvider(t, "cloudflare")
	assertEqual(t, "create base_url", p.NetworkConfig.BaseURL, "https://api.cloudflare.com/client/v4")
	assertEqual(t, "create concurrency", p.ConcurrencyAndBufferSize.Concurrency, 10)

	opts.Vars["network_config"] = map[string]interface{}{
		"base_url":                           "https://gateway.ai.cloudflare.com/v1/acct/gw/workers-ai",
		"default_request_timeout_in_seconds": 90,
		"max_retries":                        1,
	}
	opts.Vars["concurrency_and_buffer_size"] = map[string]interface{}{"concurrency": 20, "buffer_size": 40}
	opts.Vars["proxy_config"] = map[string]interface{}{"type": "http", "url": "http://proxy-b.example.com:3128"}
	applyClean("reconfigure")
	p = getProvider(t, "cloudflare")
	assertEqual(t, "reconfigure base_url", p.NetworkConfig.BaseURL, "https://gateway.ai.cloudflare.com/v1/acct/gw/workers-ai")
	assertEqual(t, "reconfigure timeout", p.NetworkConfig.DefaultRequestTimeoutInSeconds, 90)
	assertEqual(t, "reconfigure buffer_size", p.ConcurrencyAndBufferSize.BufferSize, 40)

	delete(opts.Vars, "network_config")
	delete(opts.Vars, "concurrency_and_buffer_size")
	delete(opts.Vars, "proxy_config")
	applyClean("remove blocks")
	p = getProvider(t, "cloudflare")
	assertEqual(t, "removed base_url", p.NetworkConfig.BaseURL, "")
	assertEqual(t, "removed timeout", p.NetworkConfig.DefaultRequestTimeoutInSeconds, 30)
	assertEqual(t, "removed concurrency", p.ConcurrencyAndBufferSize.Concurrency, 1000)
	if p.ProxyConfig != nil && p.ProxyConfig.Type != "none" {
		t.Errorf("removed proxy_config still active server-side: type %q", p.ProxyConfig.Type)
	}

	opts.Vars["send_back_raw_request"] = true
	applyClean("update without blocks")
	p = getProvider(t, "cloudflare")
	assertEqual(t, "unmanaged timeout preserved", p.NetworkConfig.DefaultRequestTimeoutInSeconds, 30)
	assertEqual(t, "unmanaged concurrency preserved", p.ConcurrencyAndBufferSize.Concurrency, 1000)

	bifrostRequest(t, http.MethodDelete, "/api/providers/cloudflare")
	applyClean("recreate after out-of-band delete")

	delete(opts.Vars, "account_id")
	if _, err := terraform.ApplyE(t, opts); err == nil || !strings.Contains(err.Error(), "cloudflare_key_config.account_id") {
		t.Errorf("removing cloudflare_key_config: expected a clean account_id error, got %v", err)
	}
	opts.Vars["account_id"] = "0123456789abcdef0123456789abcdef"
	applyClean("restore cloudflare_key_config")

	terraform.Destroy(t, opts)
	if status := bifrostRequest(t, http.MethodGet, "/api/providers/cloudflare"); status != http.StatusNotFound {
		t.Errorf("after destroy: GET provider returned %d, want 404", status)
	}
}

type providerState struct {
	NetworkConfig struct {
		BaseURL                        string `json:"base_url"`
		DefaultRequestTimeoutInSeconds int    `json:"default_request_timeout_in_seconds"`
	} `json:"network_config"`
	ConcurrencyAndBufferSize struct {
		Concurrency int `json:"concurrency"`
		BufferSize  int `json:"buffer_size"`
	} `json:"concurrency_and_buffer_size"`
	ProxyConfig *struct {
		Type string `json:"type"`
	} `json:"proxy_config"`
}

func getProvider(t *testing.T, name string) providerState {
	t.Helper()
	var p providerState
	body := bifrostGet(t, "/api/providers/"+name)
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("decode provider: %v (body=%s)", err, string(body))
	}
	return p
}

func bifrostGet(t *testing.T, path string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, bifrostEndpoint+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.SetBasicAuth(bifrostUsername, bifrostPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, body %s", path, resp.StatusCode, string(body))
	}
	return body
}

func bifrostRequest(t *testing.T, method, path string) int {
	t.Helper()
	req, err := http.NewRequest(method, bifrostEndpoint+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.SetBasicAuth(bifrostUsername, bifrostPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func assertEqual[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}
