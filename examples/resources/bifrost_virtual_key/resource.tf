# Virtual key with budget and rate limit, balanced across two providers.
resource "bifrost_virtual_key" "team_key" {
  name        = "team-alpha-key"
  description = "Virtual key for Team Alpha with monthly budget"
  is_active   = true

  budget = {
    max_limit        = 500.0
    reset_duration   = "1M"
    calendar_aligned = true
  }

  rate_limit = {
    request_max_limit      = 10000
    request_reset_duration = "1d"
    token_max_limit        = 5000000
    token_reset_duration   = "1d"
  }

  provider_configs = [
    {
      provider = "bedrock"
      weight   = 0.7
    },
    {
      provider = "my-openai"
      weight   = 0.3
    }
  ]
}

output "virtual_key_value" {
  value     = bifrost_virtual_key.team_key.value
  sensitive = true
}

output "virtual_key_id" {
  value = bifrost_virtual_key.team_key.id
}
