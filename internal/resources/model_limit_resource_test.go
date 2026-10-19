package resources_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/airhelp-osp/terraform-provider-bifrost/internal/provider"
)

func TestAccModelLimit_ScopeValidator(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: provider.ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "bifrost" {
  endpoint = "http://example.invalid"
}

resource "bifrost_model_limit" "test" {
  model_name = "gpt-4o"
  scope      = "team"
}
`,
				ExpectError: regexp.MustCompile(`Attribute scope value must be one of`),
			},
		},
	})
}
