# Provider configuration only. API keys are managed separately via
# bifrost_provider_key — see examples/resources/bifrost_provider_key/.

# AWS Bedrock provider.
resource "bifrost_provider" "bedrock" {
  provider_name = "bedrock"

  network_config = {
    default_request_timeout_in_seconds = 60
    max_retries                        = 3
    retry_backoff_initial_ms           = 500
    retry_backoff_max_ms               = 5000
  }
}

# Custom OpenAI-compatible provider.
resource "bifrost_provider" "openai_custom" {
  provider_name = "my-openai"

  custom_provider_config = {
    base_provider_type = "openai"
    is_key_less        = false
  }

  network_config = {
    base_url = "https://api.openai.com/v1"
  }
}

# Cloudflare Workers AI (Bifrost v2.x). No provider-level settings are required;
# the account ID is set per key via bifrost_provider_key.cloudflare_key_config.
resource "bifrost_provider" "cloudflare" {
  provider_name = "cloudflare"
}

# Custom provider on the local network (Bifrost v1.5.17+). Bifrost rejects RFC 1918
# base URLs unless allow_private_network is set, which relaxes its SSRF protection —
# use it only for trusted LAN endpoints.
resource "bifrost_provider" "llm_on_lan" {
  provider_name = "llm-on-lan"

  custom_provider_config = {
    base_provider_type = "openai"
    is_key_less        = false
  }

  network_config = {
    base_url              = "http://192.168.1.20:11434/v1"
    allow_private_network = true
  }
}
