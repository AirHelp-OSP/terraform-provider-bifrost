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
