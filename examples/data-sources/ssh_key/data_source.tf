terraform {
  required_providers {
    cloudrift = {
      source = "berops/cloudrift"
    }
  }
}

provider "cloudrift" {
  # Set CLOUDRIFT_TOKEN env var or uncomment:
  # token = "rift_..."
}

data "cloudrift_ssh_key" "primary" {
  name = "primary"
}

output "ssh_key_id" {
  value = data.cloudrift_ssh_key.primary.id
}

output "ssh_key_public_key" {
  value = data.cloudrift_ssh_key.primary.public_key
}
