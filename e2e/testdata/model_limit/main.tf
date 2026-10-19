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

variable "model_name" {
  type = string
}

variable "provider_name" {
  type = string
}

variable "budgets" {
  type = list(object({
    max_limit      = number
    reset_duration = string
  }))
}

variable "request_max_limit" {
  type    = number
  default = null
}

provider "bifrost" {
  endpoint = var.endpoint
  username = var.username
  password = var.password
}

resource "bifrost_model_limit" "test" {
  model_name    = var.model_name
  provider_name = var.provider_name
  budgets       = var.budgets

  rate_limit = var.request_max_limit == null ? null : {
    request_max_limit      = var.request_max_limit
    request_reset_duration = "1m"
  }
}

output "id" {
  value = bifrost_model_limit.test.id
}
