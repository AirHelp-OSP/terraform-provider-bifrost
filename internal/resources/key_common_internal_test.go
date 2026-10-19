package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestWildcardNotMixed(t *testing.T) {
	cases := []struct {
		name    string
		elems   []attr.Value
		wantErr bool
	}{
		{"wildcard alone", []attr.Value{types.StringValue("*")}, false},
		{"specific values", []attr.Value{types.StringValue("a"), types.StringValue("b")}, false},
		{"mixed", []attr.Value{types.StringValue("*"), types.StringValue("a")}, true},
		{"unknown element", []attr.Value{types.StringUnknown(), types.StringValue("a")}, false},
		{"wildcard with unknown element", []attr.Value{types.StringValue("*"), types.StringUnknown()}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validator.ListRequest{
				Path:        path.Root("key_ids"),
				ConfigValue: types.ListValueMust(types.StringType, tc.elems),
			}
			resp := &validator.ListResponse{}
			WildcardNotMixed().ValidateList(context.Background(), req, resp)
			if resp.Diagnostics.HasError() != tc.wantErr {
				t.Errorf("wantErr=%v, got diagnostics %v", tc.wantErr, resp.Diagnostics)
			}
		})
	}
}
