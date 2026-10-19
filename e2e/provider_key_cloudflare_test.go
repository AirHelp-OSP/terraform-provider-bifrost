package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"
)

// TestBifrostProviderKeyCloudflare exercises a Cloudflare Workers AI provider
// and key (Bifrost v2.x), including the v2.x patch semantics of key updates:
//  1. Apply the provider + a key with cloudflare_key_config and a model alias;
//     verify account_id persisted server-side and a clean re-plan.
//  2. Change account_id and drop the alias; the alias must be cleared
//     server-side (Bifrost keeps omitted fields, so the provider sends null).
//  3. Drop the key from state and re-import; the post-import apply must settle
//     to a clean plan.
func TestBifrostProviderKeyCloudflare(t *testing.T) {
	tfDir := test_structure.CopyTerraformFolderToTemp(t, ".", "testdata/provider_key_cloudflare")

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
			"model_aliases": map[string]interface{}{
				"llama": map[string]interface{}{"model_id": "@cf/meta/llama-3.1-8b-instruct"},
			},
		},
	}
	defer terraform.Destroy(t, opts)

	terraform.Apply(t, opts)
	if keyID := terraform.Output(t, opts, "key_id"); keyID == "" {
		t.Error("key_id is empty — server should have assigned a UUID on Create")
	}
	if exitCode := terraform.PlanExitCode(t, opts); exitCode != 0 {
		t.Errorf("plan after initial apply: expected no changes (exit 0), got exit %d", exitCode)
	}
	key := getCloudflareKey(t, "primary")
	if key.CloudflareKeyConfig == nil {
		t.Fatal("cloudflare_key_config not persisted")
	}
	if len(key.Aliases) != 1 {
		t.Errorf("aliases: got %d, want 1", len(key.Aliases))
	}

	opts.Vars["account_id"] = "fedcba9876543210fedcba9876543210"
	delete(opts.Vars, "model_aliases")
	terraform.Apply(t, opts)
	if got := terraform.Output(t, opts, "account_id"); got != "fedcba9876543210fedcba9876543210" {
		t.Errorf("account_id after update: got %q", got)
	}
	if exitCode := terraform.PlanExitCode(t, opts); exitCode != 0 {
		t.Errorf("plan after update: expected no changes (exit 0), got exit %d", exitCode)
	}
	if key := getCloudflareKey(t, "primary"); len(key.Aliases) != 0 {
		t.Errorf("aliases not cleared server-side after removal from config: %v", key.Aliases)
	}

	if _, err := terraform.RunTerraformCommandE(t, opts, "state", "rm", "bifrost_provider_key.cloudflare"); err != nil {
		t.Fatalf("state rm: %v", err)
	}
	importArgs := append([]string{"import"}, terraform.FormatTerraformVarsAsArgs(opts.Vars)...)
	importArgs = append(importArgs, "bifrost_provider_key.cloudflare", "cloudflare:primary")
	if _, err := terraform.RunTerraformCommandE(t, opts, importArgs...); err != nil {
		t.Fatalf("import: %v", err)
	}
	terraform.Apply(t, opts)
	if exitCode := terraform.PlanExitCode(t, opts); exitCode != 0 {
		t.Errorf("plan after post-import apply: expected no changes (exit 0), got exit %d", exitCode)
	}
}

type cloudflareKey struct {
	Name                string                     `json:"name"`
	Aliases             map[string]json.RawMessage `json:"aliases"`
	CloudflareKeyConfig *json.RawMessage           `json:"cloudflare_key_config"`
}

func getCloudflareKey(t *testing.T, keyName string) cloudflareKey {
	t.Helper()

	url := fmt.Sprintf("%s/api/providers/cloudflare/keys", bifrostEndpoint)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build keys request: %v", err)
	}
	req.SetBasicAuth(bifrostUsername, bifrostPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, body %s", url, resp.StatusCode, string(body))
	}

	var envelope struct {
		Keys []cloudflareKey `json:"keys"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode keys: %v (body=%s)", err, string(body))
	}
	for _, k := range envelope.Keys {
		if k.Name == keyName {
			return k
		}
	}
	t.Fatalf("key %q not found on provider cloudflare (body=%s)", keyName, string(body))
	return cloudflareKey{}
}
