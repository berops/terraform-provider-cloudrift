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

data "cloudrift_instance_types" "all" {}

output "instance_types" {
  description = "All available instance types with variants"
  value = [
    for it in data.cloudrift_instance_types.all.instance_types : {
      name         = it.name
      brand_short  = it.brand_short
      manufacturer = it.manufacturer
      variants = [
        for v in it.variants : {
          name              = v.name
          cpu_count         = v.cpu_count
          logical_cpu_count = v.logical_cpu_count
          gpu_count         = v.gpu_count
          vram_gb           = v.vram / 1073741824 # bytes to GB
          dram_gb           = v.dram / 1073741824
          disk_gb           = v.disk / 1073741824
          cost_per_hour     = v.cost_per_hour
          mig_profile       = v.mig_profile
          datacenters = [
            for dc in coalesce(v.datacenters, []) : {
              name       = dc.name
              count      = dc.count
              public_ips = dc.public_ips
            }
          ]
        }
      ]
    }
  ]
}
