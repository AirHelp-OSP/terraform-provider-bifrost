package resources_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/airhelp-osp/terraform-provider-bifrost/internal/provider"
)

// TestAccProvider_BufferSizeValidator verifies that the resource-level
// config validator rejects bifrost_provider configs whose buffer_size is
// less than concurrency in the concurrency_and_buffer_size block.
//
// This is a UnitTest: no network, no Bifrost server required. It proves
// the validator surfaces invalid values at plan time rather than letting
// them reach the API.
func TestAccProvider_BufferSizeValidator(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: provider.ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "bifrost" {
  endpoint = "http://example.invalid"
}

resource "bifrost_provider" "test" {
  provider_name = "openai"
  concurrency_and_buffer_size = {
    concurrency = 2000
    buffer_size = 100
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)Invalid buffer_size.*must be >= concurrency`),
			},
		},
	})
}
