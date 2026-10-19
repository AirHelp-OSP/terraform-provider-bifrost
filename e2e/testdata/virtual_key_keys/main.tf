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

# Reverse creation order, so a server that returns keys sorted would cause a
# perpetual diff unless the configured order is kept. "*" allows all keys.
variable "key_names" {
  type    = list(string)
  default = ["second", "first"]
}

provider "bifrost" {
  endpoint = var.endpoint
  username = var.username
  password = var.password
}

resource "bifrost_provider" "test" {
  provider_name = "e2e-vk-keys"

  custom_provider_config = {
    base_provider_type = "openai"
    is_key_less        = false
  }

  network_config = {
    base_url = "https://api.openai.com/v1"
  }
}

resource "bifrost_provider_key" "keys" {
  for_each = toset(["first", "second"])

  provider_name = bifrost_provider.test.provider_name
  name          = each.key
  value         = "sk-test-${each.key}-placeholder"
  weight        = 1.0
}

resource "bifrost_virtual_key" "test" {
  name = "e2e-vk-keys"

  provider_configs = [
    {
      provider       = bifrost_provider.test.provider_name
      allowed_models = ["*"]
      key_ids        = [for n in var.key_names : n == "*" ? "*" : bifrost_provider_key.keys[n].key_id]
    },
  ]
}

output "vk_id" {
  value = bifrost_virtual_key.test.id
}

output "first_key_id" {
  value = bifrost_provider_key.keys["first"].key_id
}

output "provider_name" {
  value = bifrost_provider.test.provider_name
}
