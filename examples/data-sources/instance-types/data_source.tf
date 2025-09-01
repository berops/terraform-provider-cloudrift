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

data "cloudrift_instance_types" "types" {}

output "types" {
  value = data.cloudrift_instance_types.types
}
