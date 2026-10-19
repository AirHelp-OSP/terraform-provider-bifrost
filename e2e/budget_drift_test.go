package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/gruntwork-io/terratest/modules/terraform"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"
)

// Budgets and rate limits removed outside Terraform (e.g. in the Bifrost UI)
// must show up as drift. Before the fix, an empty server-side value fell back
// to prior state, so the plan stayed clean and the limits were silently gone.
// Each limit is cleared on its own: clearing both at once would let either one
// alone make the plan non-empty.

func TestBifrostVirtualKeyLimitDrift(t *testing.T) {
	tfDir := test_structure.CopyTerraformFolderToTemp(t, ".", "testdata/virtual_key_drift")

	opts := modelLimitOptions(tfDir, map[string]interface{}{})
	defer terraform.Destroy(t, opts)

	terraform.Apply(t, opts)
	vkID := terraform.Output(t, opts, "vk_id")
	assertCleanPlan(t, opts, "create")

	bifrostPut(t, "/api/governance/virtual-keys/"+vkID, `{"budgets":[]}`)
	if got := len(getVKBudgets(t, vkID)); got != 0 {
		t.Fatalf("out-of-band clear did not remove VK budgets, %d left", got)
	}
	assertDriftDetected(t, opts, "budgets")
	terraform.Apply(t, opts)
	assertVKBudget(t, vkID, 100.0, "1M")
	assertCleanPlan(t, opts, "budgets drift repair")

	bifrostPut(t, "/api/governance/virtual-keys/"+vkID, `{"rate_limit":{}}`)
	if getVKRateLimit(t, vkID) != nil {
		t.Fatal("out-of-band clear did not remove the VK rate_limit")
	}
	assertDriftDetected(t, opts, "rate_limit")
	terraform.Apply(t, opts)
	if rl := getVKRateLimit(t, vkID); rl == nil || rl.RequestMaxLimit == nil || *rl.RequestMaxLimit != 1000 {
		t.Errorf("drift repair did not restore the VK rate_limit, got %+v", rl)
	}
	assertCleanPlan(t, opts, "rate_limit drift repair")
}

func TestBifrostModelLimitLimitDrift(t *testing.T) {
	tfDir := test_structure.CopyTerraformFolderToTemp(t, ".", "testdata/model_limit")

	opts := modelLimitOptions(tfDir, map[string]interface{}{
		"model_name":        "e2e-drift-model",
		"provider_name":     "openai",
		"budgets":           []map[string]interface{}{{"max_limit": 100, "reset_duration": "1M"}},
		"request_max_limit": 500,
	})
	defer terraform.Destroy(t, opts)

	terraform.Apply(t, opts)
	id := terraform.Output(t, opts, "id")

	bifrostPut(t, "/api/governance/model-configs/"+id, `{"budgets":[]}`)
	if got := len(getModelConfig(t, id).Budgets); got != 0 {
		t.Fatalf("out-of-band clear did not remove model limit budgets, %d left", got)
	}
	assertDriftDetected(t, opts, "budgets")
	terraform.Apply(t, opts)
	if got := getModelConfig(t, id).Budgets; len(got) != 1 || got[0].MaxLimit != 100 {
		t.Errorf("drift repair did not restore the budget, got %+v", got)
	}
	assertCleanPlan(t, opts, "budgets drift repair")

	bifrostPut(t, "/api/governance/model-configs/"+id, `{"rate_limit":{}}`)
	if getModelConfig(t, id).RateLimit != nil {
		t.Fatal("out-of-band clear did not remove the model limit rate_limit")
	}
	assertDriftDetected(t, opts, "rate_limit")
	terraform.Apply(t, opts)
	if rl := getModelConfig(t, id).RateLimit; rl == nil || rl.RequestMaxLimit == nil || *rl.RequestMaxLimit != 500 {
		t.Errorf("drift repair did not restore the model limit rate_limit, got %+v", rl)
	}
	assertCleanPlan(t, opts, "rate_limit drift repair")
}

func assertDriftDetected(t *testing.T, opts *terraform.Options, what string) {
	t.Helper()
	if exitCode := terraform.PlanExitCode(t, opts); exitCode != 2 {
		t.Errorf("plan after out-of-band %s change: expected changes (exit 2), got exit %d", what, exitCode)
	}
}

type rateLimit struct {
	RequestMaxLimit *int64 `json:"request_max_limit"`
}

func getVKRateLimit(t *testing.T, vkID string) *rateLimit {
	t.Helper()
	var envelope struct {
		VirtualKey struct {
			RateLimit *rateLimit `json:"rate_limit"`
		} `json:"virtual_key"`
	}
	body := bifrostGet(t, "/api/governance/virtual-keys/"+vkID)
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode virtual key: %v (body=%s)", err, string(body))
	}
	return envelope.VirtualKey.RateLimit
}

func bifrostPut(t *testing.T, path, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, bifrostEndpoint+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("build PUT %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(bifrostUsername, bifrostPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT %s: status %d, body %s", path, resp.StatusCode, string(respBody))
	}
}
