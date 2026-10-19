# Global spend cap and rate limit for gpt-4o on OpenAI, independent of any virtual key.
resource "bifrost_model_limit" "gpt4o" {
  model_name    = "gpt-4o"
  provider_name = "openai"

  budgets = [
    {
      max_limit        = 500
      reset_duration   = "1M"
      calendar_aligned = true
    },
    {
      max_limit      = 50
      reset_duration = "1d"
    },
  ]

  rate_limit = {
    request_max_limit      = 1000
    request_reset_duration = "1m"
  }
}

# Per-model budget within a single virtual key. The key must list the provider in provider_configs.
resource "bifrost_model_limit" "team_gpt4o" {
  model_name    = "gpt-4o"
  provider_name = "openai"
  scope         = "virtual_key"
  scope_id      = bifrost_virtual_key.team_key.id

  budgets = [
    {
      max_limit      = 25
      reset_duration = "1w"
    },
  ]
}
