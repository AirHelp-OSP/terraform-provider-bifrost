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

func TestApiRespToKeyIDs(t *testing.T) {
	keys := func(ids ...string) []bifrostclient.VKProviderConfigKey {
		out := []bifrostclient.VKProviderConfigKey{}
		for _, id := range ids {
			out = append(out, bifrostclient.VKProviderConfigKey{KeyID: id})
		}
		return out
	}
	strs := func(ids ...string) []types.String {
		out := []types.String{}
		for _, id := range ids {
			out = append(out, types.StringValue(id))
		}
		return out
	}

	cases := []struct {
		name  string
		pc    bifrostclient.VKProviderConfigResponse
		prior []types.String
		want  []types.String
	}{
		{"allow all keys", bifrostclient.VKProviderConfigResponse{AllowAllKeys: true}, nil, strs("*")},
		{"prior order kept for same set", bifrostclient.VKProviderConfigResponse{Keys: keys("a", "b")}, strs("b", "a"), strs("b", "a")},
		{"key removed out of band", bifrostclient.VKProviderConfigResponse{Keys: keys("a")}, strs("a", "b"), strs("a")},
		{"key added out of band", bifrostclient.VKProviderConfigResponse{Keys: keys("a", "b")}, strs("a"), strs("a", "b")},
		{"no keys, unset in config", bifrostclient.VKProviderConfigResponse{}, nil, nil},
		{"no keys, empty in config", bifrostclient.VKProviderConfigResponse{}, strs(), strs()},
		{"keys removed out of band", bifrostclient.VKProviderConfigResponse{}, strs("a"), strs()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := apiRespToKeyIDs(tc.pc, tc.prior)
			if (got == nil) != (tc.want == nil) || len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if !got[i].Equal(tc.want[i]) {
					t.Errorf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
