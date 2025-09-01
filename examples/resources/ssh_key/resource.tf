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

resource "cloudrift_ssh_key" "primary" {
  name       = "primary"
  public_key = file("~/.ssh/id_ed25519.pub")
}
