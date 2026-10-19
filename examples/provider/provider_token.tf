terraform {
  required_providers {
    bifrost = {
      source  = "registry.terraform.io/airhelp-osp/bifrost"
      version = "~> 0.1"
    }
  }
}

provider "bifrost" {
  endpoint = "http://localhost:8080"
  token    = "bfak_..." # or BIFROST_TOKEN env var
}
