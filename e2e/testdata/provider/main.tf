terraform {
  required_providers {
    bifrost = {
      source = "registry.terraform.io/maximhq/bifrost"
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

variable "provider_name" {
  type = string
}

variable "timeout_seconds" {
  type    = number
  default = 30
}

provider "bifrost" {
  endpoint = var.endpoint
  username = var.username
  password = var.password
}

resource "bifrost_provider" "test" {
  provider_name = var.provider_name

  keys = [
    {
      name  = "main"
      value = "sk-test-placeholder"
    }
  ]

  custom_provider_config = {
    base_provider_type = "openai"
    is_key_less        = false
  }

  network_config = {
    base_url                           = "https://api.openai.com/v1"
    default_request_timeout_in_seconds = var.timeout_seconds
  }

  concurrency_and_buffer_size = {
    concurrency = 10
    buffer_size = 100
  }
}

output "provider_name" {
  value = bifrost_provider.test.provider_name
}

output "provider_status" {
  value = bifrost_provider.test.provider_status
}

output "timeout_seconds" {
  value = bifrost_provider.test.network_config.default_request_timeout_in_seconds
}
