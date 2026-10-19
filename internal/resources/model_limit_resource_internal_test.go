package resources

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	bifrostclient "github.com/airhelp-osp/terraform-provider-bifrost/internal/client"
)

// TestModelLimitToUpdateRequest_BlocksAlwaysSent guards against removing
// budgets or rate_limit from config being a silent no-op: Bifrost leaves both
// untouched when the field is absent, so the wire payload must carry `[]` and
// `{}` explicitly.
func TestModelLimitToUpdateRequest_BlocksAlwaysSent(t *testing.T) {
	req := modelLimitToUpdateRequest(ModelLimitResourceModel{ModelName: types.StringValue("gpt-4o")})

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"budgets":[]`, `"rate_limit":{}`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("update payload missing %s: %s", want, body)
		}
	}
	if strings.Contains(string(body), `"provider"`) {
		t.Errorf("null provider_name must be omitted, got %s", body)
	}
}

func TestModelLimitResponseToModel_NullOptionals(t *testing.T) {
	m := modelLimitResponseToModel(&bifrostclient.ModelConfigResponse{
		ID:        "mc-1",
		ModelName: "*",
		Scope:     "global",
	}, nil)

	if !m.Provider.IsNull() || !m.ScopeID.IsNull() {
		t.Errorf("absent provider_name/scope_id must be null, got %v / %v", m.Provider, m.ScopeID)
	}
	if m.Budgets != nil || m.RateLimit != nil {
		t.Errorf("absent budgets/rate_limit must stay nil, got %+v / %+v", m.Budgets, m.RateLimit)
	}
}

// TestModelLimitResponseToModel_EmptyServerBudgetsIsDrift guards against
// budgets removed outside Terraform being masked by prior state.
func TestModelLimitResponseToModel_EmptyServerBudgetsIsDrift(t *testing.T) {
	prior := &ModelLimitResourceModel{
		Budgets: []ModelLimitBudgetModel{{MaxLimit: types.Float64Value(100), ResetDuration: types.StringValue("1M")}},
	}
	m := modelLimitResponseToModel(&bifrostclient.ModelConfigResponse{ID: "mc-1", ModelName: "gpt-4o", Scope: "global"}, prior)

	if m.Budgets == nil || len(m.Budgets) != 0 {
		t.Errorf("empty server list with configured budgets must yield an empty list, got %#v", m.Budgets)
	}
}
