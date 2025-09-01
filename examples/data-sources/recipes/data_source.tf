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

data "cloudrift_recipes" "recipes" {}

output "recipes" {
  value = data.cloudrift_recipes.recipes
}
