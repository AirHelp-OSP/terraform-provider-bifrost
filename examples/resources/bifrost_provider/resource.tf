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
