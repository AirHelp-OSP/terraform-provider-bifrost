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

resource "bifrost_model_limit" "first" {
  model_name    = "e2e-conflict-model"
  provider_name = "openai"
}

resource "bifrost_model_limit" "second" {
  model_name    = "e2e-conflict-model"
  provider_name = "openai"

  depends_on = [bifrost_model_limit.first]
}
