terraform {
  required_providers {
    cloudrift = {
      source = "berops/cloudrift"
      # version = "..."
    }
  }
}

provider "cloudrift" {
  # set CLOUDRIFT_TOKEN env or:
  # token = ""
}

data "cloudrift_ssh_key" "primary" {
  name = "primary"
}
