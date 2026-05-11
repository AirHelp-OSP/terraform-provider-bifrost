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

variable "provider_name" {
  type = string
}

variable "primary_weight" {
  type    = number
  default = 1.0
}

provider "bifrost" {
  endpoint = var.endpoint
  username = var.username
  password = var.password
}

resource "bifrost_provider" "test" {
  provider_name = var.provider_name

  custom_provider_config = {
    base_provider_type = "openai"
    is_key_less        = false
  }

  network_config = {
    base_url = "https://api.openai.com/v1"
  }
}

resource "bifrost_provider_key" "primary" {
  provider_name = bifrost_provider.test.provider_name
  name          = "primary"
  value         = "sk-test-primary-placeholder"
  weight        = var.primary_weight
}

resource "bifrost_provider_key" "secondary" {
  provider_name = bifrost_provider.test.provider_name
  name          = "secondary"
  value         = "sk-test-secondary-placeholder"
  weight        = 1.0
}

resource "bifrost_provider_key" "tertiary" {
  provider_name = bifrost_provider.test.provider_name
  name          = "tertiary"
  value         = "sk-test-tertiary-placeholder"
  weight        = 1.0
  enabled       = false
}

output "primary_id" {
  value = bifrost_provider_key.primary.id
}

output "primary_key_id" {
  value = bifrost_provider_key.primary.key_id
}

output "primary_weight" {
  value = bifrost_provider_key.primary.weight
}

output "tertiary_enabled" {
  value = bifrost_provider_key.tertiary.enabled
}
