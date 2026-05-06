package e2e

import (
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"
)

// Bifrost's embedded SQLite occasionally returns "database is locked" when two
// writes land back-to-back. We retry those.
var bifrostRetryable = map[string]string{
	".*database is locked.*": "Bifrost SQLite transient lock",
}

func TestBifrostProviderResource(t *testing.T) {
	tfDir := test_structure.CopyTerraformFolderToTemp(t, ".", "testdata/provider")

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
			"endpoint":        bifrostEndpoint,
			"username":        bifrostUsername,
			"password":        bifrostPassword,
			"provider_name":   "openai-e2e",
			"timeout_seconds": 30,
		},
	}
	defer terraform.Destroy(t, opts)

	// dev_overrides forbids `tofu init`; go straight to apply.
	terraform.Apply(t, opts)

	if got := terraform.Output(t, opts, "provider_name"); got != "openai-e2e" {
		t.Errorf("provider_name: got %q, want %q", got, "openai-e2e")
	}
	if got := terraform.Output(t, opts, "timeout_seconds"); got != "30" {
		t.Errorf("timeout_seconds: got %q, want %q", got, "30")
	}

	// Verify idempotence: re-plan with the same config produces no changes.
	if exitCode := terraform.PlanExitCode(t, opts); exitCode != 0 {
		t.Errorf("plan after apply: expected no changes (exit 0), got exit %d", exitCode)
	}
}

// TestBifrostProviderResource_Import exercises the full import lifecycle:
// create → drop from state → import → apply-must-succeed → idempotent re-plan.
//
// The regression this guards: pre-fix, responseToModel suppressed `network_config`
// and `concurrency_and_buffer_size` on the post-import Read because prior.<block>
// was nil. Terraform then planned an Update that 500'd server-side with
// "a record with this name already exists" (the user-reported error).
//
// We do NOT assert zero diff immediately after import. Bifrost redacts the API
// key value on GET, so any required-secret field will legitimately show as a
// diff between post-import state (null) and the user-supplied config — the
// first post-import apply is where the secret is sent. Asserting zero diff
// after the post-import apply (idempotence) is the appropriate check.
func TestBifrostProviderResource_Import(t *testing.T) {
	tfDir := test_structure.CopyTerraformFolderToTemp(t, ".", "testdata/provider")

	// Distinct provider_name from TestBifrostProviderResource so both can run
	// against the same Bifrost container without colliding on the unique name.
	const providerName = "openai-import-e2e"

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
			"endpoint":        bifrostEndpoint,
			"username":        bifrostUsername,
			"password":        bifrostPassword,
			"provider_name":   providerName,
			"timeout_seconds": 45,
		},
	}
	defer terraform.Destroy(t, opts)

	// Create the resource so we have something to import back later.
	terraform.Apply(t, opts)

	// Drop it from state without touching the remote — simulates a lost or
	// fresh workspace adopting a pre-existing Bifrost provider.
	if _, err := terraform.RunTerraformCommandE(t, opts, "state", "rm", "bifrost_provider.test"); err != nil {
		t.Fatalf("state rm failed: %v", err)
	}

	// Re-attach via import. RunTerraformCommandE does not auto-inject -var
	// from opts.Vars the way Apply/Destroy do, so format them explicitly —
	// import needs to evaluate the config to resolve the resource address.
	importArgs := append([]string{"import"}, terraform.FormatTerraformVarsAsArgs(opts.Vars)...)
	importArgs = append(importArgs, "bifrost_provider.test", providerName)
	if _, err := terraform.RunTerraformCommandE(t, opts, importArgs...); err != nil {
		t.Fatalf("import failed: %v", err)
	}

	// The regression guard: apply after import must succeed. Pre-fix, state was
	// missing nested blocks, so Terraform planned a full Update that the Bifrost
	// server rejected with "a record with this name already exists".
	terraform.Apply(t, opts)

	// Idempotence: re-plan after the post-import apply must show no changes.
	// This proves all fields (including secrets sent on the first apply) round-trip
	// cleanly through state.
	if exitCode := terraform.PlanExitCode(t, opts); exitCode != 0 {
		t.Errorf("plan after post-import apply: expected no changes (exit 0), got exit %d", exitCode)
	}
}
