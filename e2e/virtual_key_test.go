package e2e

import (
	"encoding/json"
	"io"
	"net/http"
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
	vkID := terraform.Output(t, opts, "vk_id")
	if vkID == "" {
		t.Fatal("vk_id output was empty")
	}
	if val := terraform.Output(t, opts, "vk_value"); val == "" {
		t.Error("vk_value output was empty")
	}

	// Regression guard for INFRA-1069: pre-fix, the provider sent the budget
	// under the singular `budget` JSON key while Bifrost v1.5.0 expects the
	// `budgets` array, so the server silently dropped the field and the budget
	// was never persisted. Hit /api/governance/budgets directly to confirm a
	// budget really exists on the server for this VK.
	assertVKBudget(t, vkID, 100.0, "1M")

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
	assertVKBudget(t, vkID, 250.0, "1M")

	exitCode := terraform.PlanExitCode(t, opts)
	if exitCode != 0 {
		t.Errorf("plan after apply: expected no changes (exit 0), got exit %d", exitCode)
	}
}

// bifrostBudget is the subset of a virtual key's embedded budget we assert on.
type bifrostBudget struct {
	ID            string  `json:"id"`
	MaxLimit      float64 `json:"max_limit"`
	ResetDuration string  `json:"reset_duration"`
}

// getVKBudgets fetches a virtual key from the running Bifrost container and
// returns its embedded budgets.
//
// Note: as of Bifrost v1.6.x a VK-scoped budget is stored under a
// model_config_id rather than a virtual_key_id, so it is no longer discoverable
// by filtering the global /api/governance/budgets list on virtual_key_id. The
// VK detail endpoint, which embeds the budget directly, is the canonical source.
func getVKBudgets(t *testing.T, vkID string) []bifrostBudget {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, bifrostEndpoint+"/api/governance/virtual-keys/"+vkID, nil)
	if err != nil {
		t.Fatalf("build virtual-key request: %v", err)
	}
	req.SetBasicAuth(bifrostUsername, bifrostPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/governance/virtual-keys/%s: %v", vkID, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/governance/virtual-keys/%s: status %d, body %s", vkID, resp.StatusCode, string(body))
	}
	var envelope struct {
		VirtualKey struct {
			Budgets []bifrostBudget `json:"budgets"`
		} `json:"virtual_key"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode virtual key: %v (body=%s)", err, string(body))
	}
	return envelope.VirtualKey.Budgets
}

// assertVKBudget fails the test unless the virtual key has exactly one embedded
// budget with the expected max_limit and reset_duration.
func assertVKBudget(t *testing.T, vkID string, wantMax float64, wantReset string) {
	t.Helper()
	budgets := getVKBudgets(t, vkID)
	if len(budgets) != 1 {
		t.Fatalf("expected 1 budget for vk %s, got %d: %+v", vkID, len(budgets), budgets)
	}
	got := budgets[0]
	if got.MaxLimit != wantMax {
		t.Errorf("budget.max_limit for vk %s: got %v, want %v", vkID, got.MaxLimit, wantMax)
	}
	if got.ResetDuration != wantReset {
		t.Errorf("budget.reset_duration for vk %s: got %q, want %q", vkID, got.ResetDuration, wantReset)
	}
}
