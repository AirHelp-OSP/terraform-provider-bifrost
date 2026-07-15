package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"
)

// TestBifrostProviderKeyAliases exercises the rich `model_aliases` map end-to-end
// against a real Bifrost v1.6.x container:
//  1. Apply a bedrock provider + a key with two inference-profile aliases (a
//     system-profile alias by id and an application-profile alias by ARN with
//     routing metadata); verify outputs and a clean idempotent re-plan.
//  2. Confirm the alias object fields actually persisted server-side by hitting
//     /api/providers/{provider}/keys directly (a state-looks-right / server-
//     dropped-it regression guard, mirroring the INFRA-1069 VK budget check).
//  3. Drop the key from state and re-import via "provider_name:key_name"; the
//     post-import apply must settle to a clean plan (aliases round-trip).
func TestBifrostProviderKeyAliases(t *testing.T) {
	tfDir := test_structure.CopyTerraformFolderToTemp(t, ".", "testdata/provider_key_aliases")

	const (
		providerName = "bedrock"
		arn          = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/e2e-alias"
	)

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
			"endpoint":              bifrostEndpoint,
			"username":              bifrostUsername,
			"password":              bifrostPassword,
			"provider_name":         providerName,
			"inference_profile_arn": arn,
		},
	}
	defer terraform.Destroy(t, opts)

	terraform.Apply(t, opts)

	if keyID := terraform.Output(t, opts, "key_id"); keyID == "" {
		t.Error("key_id is empty — server should have assigned a UUID on Create")
	}
	if got, want := terraform.Output(t, opts, "system_model_id"), "us.anthropic.claude-3-5-sonnet-20241022-v2:0"; got != want {
		t.Errorf("system_model_id: got %q, want %q", got, want)
	}
	if got := terraform.Output(t, opts, "app_inference_profile_arn"); got != arn {
		t.Errorf("app_inference_profile_arn: got %q, want %q", got, arn)
	}
	if got := terraform.Output(t, opts, "app_model_family"); got != "anthropic" {
		t.Errorf("app_model_family: got %q, want %q", got, "anthropic")
	}

	// Idempotence after Create — the rich alias objects must round-trip with no
	// spurious diff.
	if exitCode := terraform.PlanExitCode(t, opts); exitCode != 0 {
		t.Errorf("plan after initial apply: expected no changes (exit 0), got exit %d", exitCode)
	}

	// Server-side persistence guard: read the key straight from Bifrost and
	// confirm both aliases exist and the application alias carries the ARN.
	assertKeyAliasPersisted(t, providerName, "aliased", arn)

	// Import round-trip: drop from state, re-attach via composite ID, apply, and
	// require a clean plan.
	if _, err := terraform.RunTerraformCommandE(t, opts, "state", "rm", "bifrost_provider_key.aliased"); err != nil {
		t.Fatalf("state rm aliased: %v", err)
	}
	importID := fmt.Sprintf("%s:aliased", providerName)
	importArgs := append([]string{"import"}, terraform.FormatTerraformVarsAsArgs(opts.Vars)...)
	importArgs = append(importArgs, "bifrost_provider_key.aliased", importID)
	if _, err := terraform.RunTerraformCommandE(t, opts, importArgs...); err != nil {
		t.Fatalf("import aliased: %v", err)
	}
	terraform.Apply(t, opts)
	if exitCode := terraform.PlanExitCode(t, opts); exitCode != 0 {
		t.Errorf("plan after post-import apply: expected no changes (exit 0), got exit %d", exitCode)
	}
}

// assertKeyAliasPersisted fails unless the named key on the given provider has
// both the system and application aliases, with the application alias carrying
// the expected inference profile ARN.
func assertKeyAliasPersisted(t *testing.T, providerName, keyName, wantARN string) {
	t.Helper()

	url := fmt.Sprintf("%s/api/providers/%s/keys", bifrostEndpoint, providerName)
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
		Keys []struct {
			Name    string                     `json:"name"`
			Aliases map[string]json.RawMessage `json:"aliases"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode keys: %v (body=%s)", err, string(body))
	}

	var aliases map[string]json.RawMessage
	for _, k := range envelope.Keys {
		if k.Name == keyName {
			aliases = k.Aliases
			break
		}
	}
	if aliases == nil {
		t.Fatalf("key %q not found on provider %q (body=%s)", keyName, providerName, string(body))
	}
	if _, ok := aliases["claude-system"]; !ok {
		t.Errorf("alias claude-system not persisted (aliases=%v)", aliases)
	}
	app, ok := aliases["claude-app"]
	if !ok {
		t.Fatalf("alias claude-app not persisted (aliases=%v)", aliases)
	}
	// The application alias serializes as a rich object; assert the ARN survived
	// rather than depending on the exact SecretVar wire shape.
	if !strings.Contains(string(app), wantARN) {
		t.Errorf("claude-app alias missing inference profile ARN %q: %s", wantARN, string(app))
	}
}
