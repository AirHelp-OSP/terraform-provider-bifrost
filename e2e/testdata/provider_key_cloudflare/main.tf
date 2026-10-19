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

variable "account_id" {
  type = string
}

variable "model_aliases" {
  type = map(object({
    model_id = string
  }))
  default = null
}

provider "bifrost" {
  endpoint = var.endpoint
  username = var.username
  password = var.password
}

resource "bifrost_provider" "cloudflare" {
  provider_name = "cloudflare"
}

resource "bifrost_provider_key" "cloudflare" {
  provider_name = bifrost_provider.cloudflare.provider_name
  name          = "primary"
  value         = "cf-test-token-placeholder"
  model_aliases = var.model_aliases

  cloudflare_key_config = {
    account_id = var.account_id
  }
}

output "key_id" {
  value = bifrost_provider_key.cloudflare.key_id
}

output "account_id" {
  value = bifrost_provider_key.cloudflare.cloudflare_key_config.account_id
}
