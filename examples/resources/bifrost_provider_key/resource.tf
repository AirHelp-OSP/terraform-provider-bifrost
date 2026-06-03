# API keys for Bifrost providers. Each bifrost_provider_key maps 1:1 to a
# server-side key under /api/providers/{provider}/keys (Bifrost v1.5.0+).

# AWS Bedrock key with IAM credentials. Reference the parent provider so
# Terraform sequences creation (provider first, then keys).
#
# `value` is omitted: Bedrock authenticates via bedrock_key_config (access_key +
# secret_key or IAM-role auth) and ignores the top-level value field entirely.
resource "bifrost_provider_key" "bedrock_primary" {
  provider_name = bifrost_provider.bedrock.provider_name
  name          = "primary"

  bedrock_key_config = {
    access_key = "AKIAIOSFODNN7EXAMPLE"
    secret_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
    region     = "us-east-1"
  }

  # Map a user-facing model name to a Bedrock inference profile.
  model_aliases = {
    "claude-3-opus" = "us.anthropic.claude-3-opus-20240229-v1:0"
  }
}

# OpenAI-compatible custom provider with two weighted keys. The secret is set
# via the write-only `value` argument (never stored in state; requires
# Terraform >= 1.11 / OpenTofu >= 1.10).
resource "bifrost_provider_key" "openai_primary" {
  provider_name = bifrost_provider.openai_custom.provider_name
  name          = "primary"
  value         = "sk-my-api-key-primary"
  models        = ["gpt-4o", "gpt-4o-mini"]
  weight        = 0.7
}

resource "bifrost_provider_key" "openai_secondary" {
  provider_name = bifrost_provider.openai_custom.provider_name
  name          = "secondary"
  value         = "sk-my-api-key-secondary"
  models        = ["*"] # allow all models (v1.5.0: [] now means deny-all)
  weight        = 0.3
}
