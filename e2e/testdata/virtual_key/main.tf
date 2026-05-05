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

variable "vk_name" {
  type = string
}

variable "description" {
  type    = string
  default = "e2e test key"
}

variable "budget_limit" {
  type    = number
  default = 100
}

provider "bifrost" {
  endpoint = var.endpoint
  username = var.username
  password = var.password
}

resource "bifrost_virtual_key" "test" {
  name        = var.vk_name
  description = var.description
  is_active   = true

  budget = {
    max_limit      = var.budget_limit
    reset_duration = "1M"
  }
}

output "vk_id" {
  value = bifrost_virtual_key.test.id
}

output "vk_value" {
  value     = bifrost_virtual_key.test.value
  sensitive = true
}

output "description" {
  value = bifrost_virtual_key.test.description
}

output "budget_limit" {
  value = bifrost_virtual_key.test.budget.max_limit
}
