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

data "cloudrift_recipes" "all" {}

output "recipes" {
  description = "All available recipes with tags"
  value = [
    for g in data.cloudrift_recipes.all.groups : {
      name        = g.name
      description = g.description
      recipes = [
        for r in g.recipes : {
          name        = r.name
          description = r.description
          tags        = r.tags
        }
      ]
    }
  ]
}
