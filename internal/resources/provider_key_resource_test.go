package resources_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/airhelp-osp/terraform-provider-bifrost/internal/provider"
)

// These are UnitTests: no network, no Bifrost server. They prove the
// plan-time validation on the v2 nested `model_aliases` attribute fires before
// any API call, so misconfigurations surface as a plan error rather than at
// apply time.

// TestAccProviderKey_ModelFamilyValidator verifies the OneOf validator on
// model_aliases.<key>.model_family rejects an unknown family.
func TestAccProviderKey_ModelFamilyValidator(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: provider.ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "bifrost" {
  endpoint = "http://example.invalid"
}

resource "bifrost_provider_key" "test" {
  provider_name = "bedrock"
  name          = "primary"
  model_aliases = {
    "claude" = {
      model_id     = "anthropic.claude-3-5-sonnet-20241022-v2:0"
      model_family = "klingon" # not a valid schemas.ModelFamily
    }
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)model_family.*value must be one of`),
			},
		},
	})
}

// TestAccProviderKey_ModelIDRequired verifies that model_id is required within
// each alias object (omitting it is a config error, not a silent empty send).
func TestAccProviderKey_ModelIDRequired(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: provider.ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "bifrost" {
  endpoint = "http://example.invalid"
}

resource "bifrost_provider_key" "test" {
  provider_name = "bedrock"
  name          = "primary"
  model_aliases = {
    "claude" = {
      model_name = "claude-3-5-sonnet-20241022"
    }
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)(model_id.*required|Missing.*model_id|argument "model_id" is required)`),
			},
		},
	})
}

// TestAccProviderKey_InferenceProfileARNRequiresBedrock verifies the
// resource-level config validator rejects inference_profile_arn on a
// non-bedrock provider key.
func TestAccProviderKey_InferenceProfileARNRequiresBedrock(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: provider.ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "bifrost" {
  endpoint = "http://example.invalid"
}

resource "bifrost_provider_key" "test" {
  provider_name = "openai"
  name          = "primary"
  model_aliases = {
    "claude" = {
      model_id              = "anthropic.claude-3-5-sonnet-20241022-v2:0"
      inference_profile_arn = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc"
    }
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)inference_profile_arn requires the bedrock provider`),
			},
		},
	})
}

// TestAccProviderKey_InferenceProfileARNMissingProviderName verifies that when
// provider_name is omitted (null), the config validator does NOT run — it must
// not stack a secondary "inference_profile_arn requires the bedrock provider"
// diagnostic on top of the framework's primary "missing required argument"
// error. The surfaced error must be the missing-argument one.
func TestAccProviderKey_InferenceProfileARNMissingProviderName(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: provider.ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "bifrost" {
  endpoint = "http://example.invalid"
}

resource "bifrost_provider_key" "test" {
  name = "primary"
  model_aliases = {
    "claude" = {
      model_id              = "anthropic.claude-3-5-sonnet-20241022-v2:0"
      inference_profile_arn = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc"
    }
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)(The argument "provider_name" is required|Missing required argument)`),
			},
		},
	})
}
