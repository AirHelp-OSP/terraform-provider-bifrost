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

variable "inference_profile_arn" {
  type = string
}

variable "app_description" {
  type    = string
  default = "prod Claude on our AWS tenant"
}

provider "bifrost" {
  endpoint = var.endpoint
  username = var.username
  password = var.password
}

# Built-in AWS Bedrock provider (no custom_provider_config — bedrock is a
# first-class ModelProvider). Credentials live on the key below.
resource "bifrost_provider" "bedrock" {
  provider_name = var.provider_name
}

# A single Bedrock key exposing two aliases based on inference profiles:
#   - claude-system: a cross-region *system* inference profile, addressed by id.
#   - claude-app:    an *application* inference profile, addressed by ARN, with
#                    routing/pricing metadata (model_name + model_family).
resource "bifrost_provider_key" "aliased" {
  provider_name = bifrost_provider.bedrock.provider_name
  name          = "aliased"

  bedrock_key_config = {
    access_key = "AKIAIOSFODNN7EXAMPLE"
    secret_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
    region     = "us-east-1"
  }

  model_aliases = {
    "claude-system" = {
      model_id = "us.anthropic.claude-3-5-sonnet-20241022-v2:0"
    }
    "claude-app" = {
      model_id              = "anthropic.claude-3-5-sonnet-20241022-v2:0"
      inference_profile_arn = var.inference_profile_arn
      model_name            = "claude-3-5-sonnet-20241022"
      model_family          = "anthropic"
      description           = var.app_description
    }
  }
}

output "key_id" {
  value = bifrost_provider_key.aliased.key_id
}

output "id" {
  value = bifrost_provider_key.aliased.id
}

output "system_model_id" {
  value = bifrost_provider_key.aliased.model_aliases["claude-system"].model_id
}

output "app_inference_profile_arn" {
  value = bifrost_provider_key.aliased.model_aliases["claude-app"].inference_profile_arn
}

output "app_model_family" {
  value = bifrost_provider_key.aliased.model_aliases["claude-app"].model_family
}
