# AWS Bedrock provider with IAM key credentials.
resource "bifrost_provider" "bedrock" {
  provider_name = "bedrock"

  keys = [
    {
      name  = "primary"
      value = "" # Bedrock uses bedrock_key_config, not a raw API key
      bedrock_key_config = {
        access_key = "AKIAIOSFODNN7EXAMPLE"
        secret_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
        region     = "us-east-1"
      }
    }
  ]

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

  keys = [
    {
      name   = "key1"
      value  = "sk-my-api-key"
      models = ["gpt-4o", "gpt-4o-mini"]
      weight = 1.0
    }
  ]

  custom_provider_config = {
    base_provider_type = "openai"
    is_key_less        = false
  }

  network_config = {
    base_url = "https://api.openai.com/v1"
  }
}
