package e2e

import (
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"
)

func TestBifrostVirtualKeyResource(t *testing.T) {
	tfDir := test_structure.CopyTerraformFolderToTemp(t, ".", "testdata/virtual_key")

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
			"endpoint":     bifrostEndpoint,
			"username":     bifrostUsername,
			"password":     bifrostPassword,
			"vk_name":      "e2e-vk",
			"description":  "initial",
			"budget_limit": 100,
		},
	}
	defer terraform.Destroy(t, opts)

	terraform.Apply(t, opts)

	if got := terraform.Output(t, opts, "description"); got != "initial" {
		t.Errorf("description: got %q, want %q", got, "initial")
	}
	if got := terraform.Output(t, opts, "budget_limit"); got != "100" {
		t.Errorf("budget_limit: got %q, want %q", got, "100")
	}
	if id := terraform.Output(t, opts, "vk_id"); id == "" {
		t.Error("vk_id output was empty")
	}
	if val := terraform.Output(t, opts, "vk_value"); val == "" {
		t.Error("vk_value output was empty")
	}

	// Update — change description and budget.
	opts.Vars["description"] = "updated"
	opts.Vars["budget_limit"] = 250
	terraform.Apply(t, opts)

	if got := terraform.Output(t, opts, "description"); got != "updated" {
		t.Errorf("after update, description: got %q, want %q", got, "updated")
	}
	if got := terraform.Output(t, opts, "budget_limit"); got != "250" {
		t.Errorf("after update, budget_limit: got %q, want %q", got, "250")
	}

	exitCode := terraform.PlanExitCode(t, opts)
	if exitCode != 0 {
		t.Errorf("plan after apply: expected no changes (exit 0), got exit %d", exitCode)
	}
}
