package resources

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	bifrostclient "github.com/airhelp-osp/terraform-provider-bifrost/internal/client"
)

// TestApiRespToBudgets_EmptyServerListIsDrift guards against budgets removed
// outside Terraform being masked by prior state.
func TestApiRespToBudgets_EmptyServerListIsDrift(t *testing.T) {
	prior := []VKBudgetModel{{MaxLimit: types.Float64Value(100), ResetDuration: types.StringValue("1M")}}

	got := apiRespToBudgets(nil, prior)
	if got == nil || len(got) != 0 {
		t.Errorf("empty server list with configured budgets must yield an empty list, got %#v", got)
	}
	if got := apiRespToBudgets(nil, nil); got != nil {
		t.Errorf("empty server list with no configured budgets must stay null, got %#v", got)
	}
}

// TestApiRespToBudgets_DeprecatedCalendarAlignedFromPrior: Bifrost never stores
// the per-budget flag, so it must come from prior state to avoid a perpetual diff.
func TestApiRespToBudgets_DeprecatedCalendarAlignedFromPrior(t *testing.T) {
	prior := []VKBudgetModel{{
		MaxLimit:        types.Float64Value(100),
		ResetDuration:   types.StringValue("1M"),
		CalendarAligned: types.BoolValue(true),
	}}
	got := apiRespToBudgets([]bifrostclient.VKBudget{
		{MaxLimit: 100, ResetDuration: "1M"},
		{MaxLimit: 10, ResetDuration: "1d"},
	}, prior)

	if !got[0].CalendarAligned.ValueBool() {
		t.Error("calendar_aligned must be carried over from the prior budget with the same reset_duration")
	}
	if got[1].CalendarAligned.IsNull() || got[1].CalendarAligned.ValueBool() {
		t.Errorf("calendar_aligned without a prior budget must default to false, got %v", got[1].CalendarAligned)
	}
}

// TestVKModelToUpdateRequest_CalendarAlignedAlwaysSent: Bifrost leaves the
// setting unchanged when the field is absent, so turning it off must send false.
func TestVKModelToUpdateRequest_CalendarAlignedAlwaysSent(t *testing.T) {
	req := vkModelToUpdateRequest(VirtualKeyResourceModel{CalendarAligned: types.BoolValue(false)})

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"calendar_aligned":false`) {
		t.Errorf("update payload must carry calendar_aligned=false, got %s", body)
	}
}
