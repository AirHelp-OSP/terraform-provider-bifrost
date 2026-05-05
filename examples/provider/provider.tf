terraform {
  required_providers {
    bifrost = {
      source  = "registry.terraform.io/maximhq/bifrost"
      version = "~> 0.1"
    }
  }
}

provider "bifrost" {
  endpoint = "http://localhost:8080"
  username = "admin"
  password = "testpassword123"
}
