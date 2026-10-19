package resources_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/airhelp-osp/terraform-provider-bifrost/internal/provider"
)

// TestAccVirtualKey_ConflictingAttributes verifies that the resource-level
// config validator rejects configs that set both team_id and customer_id.
//
// This is a UnitTest: no network, no Bifrost server required. It proves the
// validator wiring is correct and that validation errors surface at plan time
// rather than at apply time (which would burn API quota).
func TestAccVirtualKey_ConflictingAttributes(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: provider.ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "bifrost" {
  endpoint = "http://example.invalid"
}

resource "bifrost_virtual_key" "test" {
  name        = "test"
  team_id     = "team-1"
  customer_id = "customer-1"
}
`,
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination.*team_id.*customer_id`),
			},
		},
	})
}

// TestAccProvider_ProxyTypeValidator verifies that the OneOf validator on
// bifrost_provider.proxy_config.type rejects an unknown proxy type before
// reaching the API.
func TestAccProvider_ProxyTypeValidator(t *testing.T) {
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
  proxy_config = {
    type = "tor" # not in {none, http, socks5, environment}
  }
}
`,
				ExpectError: regexp.MustCompile(`Attribute proxy_config\.type value must be one of`),
			},
		},
	})
}

// TestAccVirtualKey_RateLimitNeedsAMaxLimit verifies an empty rate_limit is
// rejected at plan time: Bifrost would delete it and the apply would fail with
// an inconsistent result. An omitted rate_limit must still plan cleanly.
func TestAccVirtualKey_RateLimitNeedsAMaxLimit(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: provider.ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "bifrost" {
  endpoint = "http://example.invalid"
}

resource "bifrost_virtual_key" "test" {
  name = "test"
  rate_limit = {
    request_reset_duration = "1m"
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)At least one attribute out of.*request_max_limit`),
			},
			{
				Config: `
provider "bifrost" {
  endpoint = "http://example.invalid"
}

resource "bifrost_virtual_key" "test" {
  name = "test"
}
`,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
