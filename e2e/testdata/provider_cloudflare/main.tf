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

variable "network_config" {
  type = object({
    base_url                           = string
    default_request_timeout_in_seconds = number
    max_retries                        = number
  })
  default = null
}

variable "concurrency_and_buffer_size" {
  type = object({
    concurrency = number
    buffer_size = number
  })
  default = null
}

variable "proxy_config" {
  type = object({
    type = string
    url  = string
  })
  default = null
}

variable "send_back_raw_request" {
  type    = bool
  default = false
}

variable "account_id" {
  type    = string
  default = null
}

provider "bifrost" {
  endpoint = var.endpoint
  username = var.username
  password = var.password
}

resource "bifrost_provider" "cloudflare" {
  provider_name               = "cloudflare"
  network_config              = var.network_config
  concurrency_and_buffer_size = var.concurrency_and_buffer_size
  proxy_config                = var.proxy_config
  send_back_raw_request       = var.send_back_raw_request
}

resource "bifrost_provider_key" "cloudflare" {
  provider_name         = bifrost_provider.cloudflare.provider_name
  name                  = "primary"
  value                 = "cf-test-token-placeholder"
  cloudflare_key_config = var.account_id == null ? null : { account_id = var.account_id }
}
