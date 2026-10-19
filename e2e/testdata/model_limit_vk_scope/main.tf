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

variable "vk_description" {
  type = string
}

provider "bifrost" {
  endpoint = var.endpoint
  username = var.username
  password = var.password
}

resource "bifrost_provider" "openai" {
  provider_name = "openai"
}

resource "bifrost_virtual_key" "test" {
  name        = "e2e-model-limit-vk"
  description = var.vk_description

  provider_configs = [
    {
      provider       = bifrost_provider.openai.provider_name
      allowed_models = ["*"]
    },
  ]
}

resource "bifrost_model_limit" "test" {
  model_name    = "gpt-4o"
  provider_name = bifrost_provider.openai.provider_name
  scope         = "virtual_key"
  scope_id      = bifrost_virtual_key.test.id

  budgets = [
    {
      max_limit      = 10
      reset_duration = "1d"
    },
  ]
}

output "id" {
  value = bifrost_model_limit.test.id
}
