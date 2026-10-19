terraform {
  required_providers {
    bifrost = {
      source = "registry.terraform.io/airhelp-osp/bifrost"
    }
  }
}

variable "endpoint" {
  type = string
}

variable "username" {
  type = string
}

variable "password" {
  type      = string
  sensitive = true
}

provider "bifrost" {
  endpoint = var.endpoint
  username = var.username
  password = var.password
}

resource "bifrost_provider" "anthropic" {
  provider_name = "anthropic"
}

resource "bifrost_virtual_key" "test" {
  name = "e2e-vk-drift"

  budgets = [
    {
      max_limit      = 100
      reset_duration = "1M"
    },
  ]

  rate_limit = {
    request_max_limit      = 1000
    request_reset_duration = "1m"
  }

  provider_configs = [
    {
      provider       = bifrost_provider.anthropic.provider_name
      allowed_models = ["*"]
      budgets = [
        {
          max_limit      = 30
          reset_duration = "1d"
        },
      ]
    },
  ]
}

output "vk_id" {
  value = bifrost_virtual_key.test.id
}
