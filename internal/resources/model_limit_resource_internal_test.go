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

// TestModelLimitResponseToModel_PreservesCalendarAligned covers Bifrost not
// echoing calendar_aligned on budgets, which would otherwise produce a
// perpetual diff for calendar-aligned budgets.
func TestModelLimitResponseToModel_PreservesCalendarAligned(t *testing.T) {
	prior := &ModelLimitResourceModel{
		Budgets: []VKBudgetModel{{
			MaxLimit:        types.Float64Value(100),
			ResetDuration:   types.StringValue("1M"),
			CalendarAligned: types.BoolValue(true),
		}},
	}
	m := modelLimitResponseToModel(&bifrostclient.ModelConfigResponse{
		ID:        "mc-1",
		ModelName: "gpt-4o",
		Scope:     "global",
		Budgets:   []bifrostclient.VKBudget{{MaxLimit: 100, ResetDuration: "1M"}},
	}, prior)

	if len(m.Budgets) != 1 || !m.Budgets[0].CalendarAligned.ValueBool() {
		t.Errorf("calendar_aligned must be preserved from prior state, got %+v", m.Budgets)
	}
}
