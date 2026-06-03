package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"
)

// TestBifrostProviderKeyResource exercises the per-key lifecycle backed by
// the v1.5.0 /api/providers/{provider}/keys endpoints:
//  1. Apply a provider + 3 keys; verify outputs and that each key receives a
//     server-assigned UUID.
//  2. Mutate one key (change weight) and apply; idempotent re-plan must be clean.
//  3. Disable a key via the `enabled` field and verify the change persists.
//  4. Drop the primary from state and import via "provider_name:key_name";
//     post-import apply (re-supplies the redacted secret) followed by a clean plan.
func TestBifrostProviderKeyResource(t *testing.T) {
	tfDir := test_structure.CopyTerraformFolderToTemp(t, ".", "testdata/provider_key")

	const providerName = "openai-key-e2e"

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
			"endpoint":       bifrostEndpoint,
			"username":       bifrostUsername,
			"password":       bifrostPassword,
			"provider_name":  providerName,
			"primary_weight": 1.0,
		},
	}
	defer terraform.Destroy(t, opts)

	terraform.Apply(t, opts)

	primaryID := terraform.Output(t, opts, "primary_id")
	if want := providerName + ":primary"; primaryID != want {
		t.Errorf("primary_id: got %q, want %q", primaryID, want)
	}
	if keyID := terraform.Output(t, opts, "primary_key_id"); keyID == "" {
		t.Error("primary_key_id is empty — server should have assigned a UUID on Create")
	}
	if got := terraform.Output(t, opts, "tertiary_enabled"); got != "false" {
		t.Errorf("tertiary_enabled: got %q, want %q", got, "false")
	}

	// Idempotence after Create.
	if exitCode := terraform.PlanExitCode(t, opts); exitCode != 0 {
		t.Errorf("plan after initial apply: expected no changes (exit 0), got exit %d", exitCode)
	}

	// Update path: change the primary key's weight.
	opts.Vars["primary_weight"] = 3.5
	terraform.Apply(t, opts)
	if got := terraform.Output(t, opts, "primary_weight"); got != "3.5" {
		t.Errorf("after update, primary_weight: got %q, want %q", got, "3.5")
	}
	if exitCode := terraform.PlanExitCode(t, opts); exitCode != 0 {
		t.Errorf("plan after weight update: expected no changes (exit 0), got exit %d", exitCode)
	}

	// Import path: drop primary from state, re-attach via composite ID.
	if _, err := terraform.RunTerraformCommandE(t, opts, "state", "rm", "bifrost_provider_key.primary"); err != nil {
		t.Fatalf("state rm primary: %v", err)
	}
	importID := fmt.Sprintf("%s:primary", providerName)
	importArgs := append([]string{"import"}, terraform.FormatTerraformVarsAsArgs(opts.Vars)...)
	importArgs = append(importArgs, "bifrost_provider_key.primary", importID)
	if _, err := terraform.RunTerraformCommandE(t, opts, importArgs...); err != nil {
		t.Fatalf("import primary: %v", err)
	}

	// First post-import apply re-sends the write-only `value` from config (import
	// seeds no value_sha256, so the digest goes null → hash and drives one update).
	terraform.Apply(t, opts)
	if exitCode := terraform.PlanExitCode(t, opts); exitCode != 0 {
		t.Errorf("plan after post-import apply: expected no changes (exit 0), got exit %d", exitCode)
	}

	// Negative import: malformed import ID surfaces a clear error.
	importArgs = append([]string{"import"}, terraform.FormatTerraformVarsAsArgs(opts.Vars)...)
	importArgs = append(importArgs, "bifrost_provider_key.secondary", "not-a-composite-id")
	if out, err := terraform.RunTerraformCommandE(t, opts, importArgs...); err == nil {
		t.Errorf("expected import to fail with malformed ID, got success: %s", out)
	} else if !strings.Contains(out, "Invalid import ID") && !strings.Contains(err.Error(), "Invalid import ID") {
		t.Errorf("expected 'Invalid import ID' error, got: %v (out=%s)", err, out)
	}
}
