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

resource "cloudrift_ssh_key" "primary" {
  name       = "primary"
  public_key = trimspace(file("~/.ssh/id_ed25519.pub"))
}
