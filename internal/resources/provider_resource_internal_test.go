package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestModelKeysToAPI_MatchesIDsByName guards the regression where reordering
// `keys` in config caused the wrong server-side key to be updated.
//
// `keys` is a ListNestedAttribute and `keys[*].id` uses UseStateForUnknown,
// which preserves prior state values *by list index*. If the user reorders
// keys, plan-time IDs no longer match their original keys. modelKeysToAPI
// must therefore re-associate IDs by the stable `name` field before sending
// to Bifrost.
func TestModelKeysToAPI_MatchesIDsByName(t *testing.T) {
	prior := &ProviderResourceModel{
		Keys: []KeyModel{
			{ID: types.StringValue("id-A"), Name: types.StringValue("A")},
			{ID: types.StringValue("id-B"), Name: types.StringValue("B")},
			{ID: types.StringValue("id-C"), Name: types.StringValue("C")},
		},
	}

	// Plan reorders the list. With UseStateForUnknown index-carryover,
	// the plan's ID at position 0 would be "id-A" — but the name there is
	// now "C", which would cause Bifrost to update the wrong key.
	plan := []KeyModel{
		{ID: types.StringValue("id-A"), Name: types.StringValue("C"), Value: types.StringValue("vC"), Weight: types.Float64Value(1.0)},
		{ID: types.StringValue("id-C"), Name: types.StringValue("A"), Value: types.StringValue("vA"), Weight: types.Float64Value(1.0)},
		{ID: types.StringValue("id-B"), Name: types.StringValue("B"), Value: types.StringValue("vB"), Weight: types.Float64Value(1.0)},
	}

	got, diags := modelKeysToAPI(context.Background(), plan, priorKeyIDsByName(prior))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	want := map[string]string{"A": "id-A", "B": "id-B", "C": "id-C"}
	for _, k := range got {
		if k.ID != want[k.Name] {
			t.Errorf("key %q: got id=%q, want id=%q", k.Name, k.ID, want[k.Name])
		}
	}
}

// TestModelKeysToAPI_NoPriorState_LeavesIDEmpty verifies Create-path behavior:
// with no prior state, IDs in the request are empty strings (server assigns).
func TestModelKeysToAPI_NoPriorState_LeavesIDEmpty(t *testing.T) {
	plan := []KeyModel{
		{ID: types.StringNull(), Name: types.StringValue("new"), Value: types.StringValue("v"), Weight: types.Float64Value(1.0)},
	}

	got, diags := modelKeysToAPI(context.Background(), plan, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(got) != 1 || got[0].ID != "" {
		t.Errorf("expected empty ID for new key on Create, got %+v", got)
	}
}

// TestModelKeysToAPI_NewKeyAlongsideExisting verifies that adding a key
// during Update preserves IDs for existing keys but leaves the new key's
// ID empty so the server can assign one.
func TestModelKeysToAPI_NewKeyAlongsideExisting(t *testing.T) {
	prior := &ProviderResourceModel{
		Keys: []KeyModel{
			{ID: types.StringValue("id-A"), Name: types.StringValue("A")},
		},
	}
	plan := []KeyModel{
		{ID: types.StringValue("id-A"), Name: types.StringValue("A"), Value: types.StringValue("vA"), Weight: types.Float64Value(1.0)},
		{ID: types.StringNull(), Name: types.StringValue("B"), Value: types.StringValue("vB"), Weight: types.Float64Value(1.0)},
	}

	got, diags := modelKeysToAPI(context.Background(), plan, priorKeyIDsByName(prior))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	wantID := map[string]string{"A": "id-A", "B": ""}
	for _, k := range got {
		if k.ID != wantID[k.Name] {
			t.Errorf("key %q: got id=%q, want id=%q", k.Name, k.ID, wantID[k.Name])
		}
	}
}
