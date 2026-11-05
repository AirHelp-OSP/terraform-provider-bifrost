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

# Bifrost's SSRF check rejects RFC1918 base URLs unless this is set.
variable "allow_private_network" {
  type    = bool
  default = true
}

provider "bifrost" {
  endpoint = var.endpoint
  username = var.username
  password = var.password
}

# RFC1918 address on purpose: nothing listens there, the URL only needs to
# pass Bifrost's create-time validation.
resource "bifrost_provider" "test" {
  provider_name = var.provider_name

  custom_provider_config = {
    base_provider_type = "openai"
    is_key_less        = true
  }

  network_config = {
    base_url              = "http://192.168.0.10:8080/v1"
    allow_private_network = var.allow_private_network
  }
}

output "provider_name" {
  value = bifrost_provider.test.provider_name
}

output "allow_private_network" {
  value = bifrost_provider.test.network_config.allow_private_network
}
