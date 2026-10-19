package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"
)

// TestBifrostModelLimitResource covers create, in-place update, removal of
// budgets/rate_limit, replacement on provider change and import, asserting
// against the Bifrost API rather than Terraform outputs alone.
func TestBifrostModelLimitResource(t *testing.T) {
	tfDir := test_structure.CopyTerraformFolderToTemp(t, ".", "testdata/model_limit")

	opts := modelLimitOptions(tfDir, map[string]interface{}{
		"model_name":        "gpt-4o",
		"provider_name":     "openai",
		"budgets":           []map[string]interface{}{{"max_limit": 100, "reset_duration": "1M"}},
		"request_max_limit": 500,
	})
	defer terraform.Destroy(t, opts)

	terraform.Apply(t, opts)
	id := terraform.Output(t, opts, "id")
	mc := getModelConfig(t, id)
	if mc.ModelName != "gpt-4o" || len(mc.Budgets) != 1 || mc.Budgets[0].MaxLimit != 100 {
		t.Errorf("after create: unexpected model config %+v", mc)
	}
	if mc.RateLimit == nil || mc.RateLimit.RequestMaxLimit == nil || *mc.RateLimit.RequestMaxLimit != 500 {
		t.Errorf("after create: rate_limit not persisted, got %+v", mc.RateLimit)
	}
	assertCleanPlan(t, opts, "create")

	opts.Vars["model_name"] = "gpt-4o-mini"
	opts.Vars["budgets"] = []map[string]interface{}{
		{"max_limit": 250, "reset_duration": "1M"},
		{"max_limit": 20, "reset_duration": "1d"},
	}
	terraform.Apply(t, opts)
	if got := terraform.Output(t, opts, "id"); got != id {
		t.Errorf("in-place update replaced the resource: id %q -> %q", id, got)
	}
	mc = getModelConfig(t, id)
	if mc.ModelName != "gpt-4o-mini" || len(mc.Budgets) != 2 {
		t.Errorf("after update: unexpected model config %+v", mc)
	}
	assertCleanPlan(t, opts, "update")

	opts.Vars["budgets"] = []map[string]interface{}{}
	delete(opts.Vars, "request_max_limit")
	terraform.Apply(t, opts)
	mc = getModelConfig(t, id)
	if len(mc.Budgets) != 0 || mc.RateLimit != nil {
		t.Errorf("after removal: budgets/rate_limit still on server: %+v / %+v", mc.Budgets, mc.RateLimit)
	}
	assertCleanPlan(t, opts, "removal")

	opts.Vars["provider_name"] = "anthropic"
	opts.Vars["budgets"] = []map[string]interface{}{{"max_limit": 50, "reset_duration": "1w"}}
	terraform.Apply(t, opts)
	newID := terraform.Output(t, opts, "id")
	if newID == id {
		t.Errorf("provider_name change must replace the resource, id stayed %q", id)
	}
	assertCleanPlan(t, opts, "replacement")

	if _, err := terraform.RunTerraformCommandE(t, opts, "state", "rm", "bifrost_model_limit.test"); err != nil {
		t.Fatalf("state rm failed: %v", err)
	}
	importArgs := append([]string{"import"}, terraform.FormatTerraformVarsAsArgs(opts.Vars)...)
	importArgs = append(importArgs, "bifrost_model_limit.test", newID)
	if _, err := terraform.RunTerraformCommandE(t, opts, importArgs...); err != nil {
		t.Fatalf("import failed: %v", err)
	}
	assertCleanPlan(t, opts, "import")
}

// TestBifrostModelLimitVKScope guards against a virtual key update wiping a
// virtual_key-scoped model limit: Bifrost reconciles VK-scoped model configs
// on every VK PUT.
func TestBifrostModelLimitVKScope(t *testing.T) {
	tfDir := test_structure.CopyTerraformFolderToTemp(t, ".", "testdata/model_limit_vk_scope")

	opts := modelLimitOptions(tfDir, map[string]interface{}{"vk_description": "initial"})
	defer terraform.Destroy(t, opts)

	terraform.Apply(t, opts)
	id := terraform.Output(t, opts, "id")

	opts.Vars["vk_description"] = "updated"
	terraform.Apply(t, opts)

	mc := getModelConfig(t, id)
	if mc.Scope != "virtual_key" || len(mc.Budgets) != 1 {
		t.Errorf("VK-scoped model limit changed by VK update: %+v", mc)
	}
	assertCleanPlan(t, opts, "VK update")
}

func TestBifrostModelLimitConflict(t *testing.T) {
	tfDir := test_structure.CopyTerraformFolderToTemp(t, ".", "testdata/model_limit_conflict")

	opts := modelLimitOptions(tfDir, map[string]interface{}{})
	defer terraform.Destroy(t, opts)

	_, err := terraform.InitAndApplyE(t, opts)
	if err == nil || !regexp.MustCompile(`Model limit already exists`).MatchString(err.Error()) {
		t.Errorf("expected 'Model limit already exists' error, got %v", err)
	}
}

func modelLimitOptions(tfDir string, vars map[string]interface{}) *terraform.Options {
	vars["endpoint"] = bifrostEndpoint
	vars["username"] = bifrostUsername
	vars["password"] = bifrostPassword
	return &terraform.Options{
		TerraformDir:             tfDir,
		TerraformBinary:          "tofu",
		RetryableTerraformErrors: bifrostRetryable,
		MaxRetries:               3,
		TimeBetweenRetries:       2 * time.Second,
		EnvVars: map[string]string{
			"TF_CLI_CONFIG_FILE": tofuRCPath,
		},
		Vars: vars,
	}
}

func assertCleanPlan(t *testing.T, opts *terraform.Options, step string) {
	t.Helper()
	if exitCode := terraform.PlanExitCode(t, opts); exitCode != 0 {
		t.Errorf("plan after %s: expected no changes (exit 0), got exit %d", step, exitCode)
	}
}

type modelConfig struct {
	ModelName string          `json:"model_name"`
	Scope     string          `json:"scope"`
	Budgets   []bifrostBudget `json:"budgets"`
	RateLimit *struct {
		RequestMaxLimit *int64 `json:"request_max_limit"`
	} `json:"rate_limit"`
}

func getModelConfig(t *testing.T, id string) modelConfig {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, bifrostEndpoint+"/api/governance/model-configs/"+id, nil)
	if err != nil {
		t.Fatalf("build model-config request: %v", err)
	}
	req.SetBasicAuth(bifrostUsername, bifrostPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/governance/model-configs/%s: %v", id, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/governance/model-configs/%s: status %d, body %s", id, resp.StatusCode, string(body))
	}
	var envelope struct {
		ModelConfig modelConfig `json:"model_config"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode model config: %v (body=%s)", err, string(body))
	}
	return envelope.ModelConfig
}
