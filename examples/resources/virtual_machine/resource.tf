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

  # Team ID for team-scoped operations.
  # Set CLOUDRIFT_TEAM_ID env var or uncomment:
  # team_id = "your-team-uuid"
}

resource "cloudrift_ssh_key" "primary" {
  name       = "primary"
  public_key = trimspace(file("~/.ssh/id_ed25519.pub"))
}

resource "cloudrift_virtual_machine" "machine0" {
  recipe     = "Ubuntu 22.04 Server (NVidia)"
  datacenter = "us-east-nc-nr-1"
  # Instance types change frequently. To check available instances, use:
  # data "cloudrift_instance_types" "all" {}
  # output "instance_types" { value = data.cloudrift_instance_types.all.instance_types }
  instance_type = "rtx49-7-50-500-nr.1"
  ssh_key_id    = cloudrift_ssh_key.primary.id

  metadata = {
    startup_commands = base64encode(<<EOF
#!/bin/bash
# allow login as root.
echo 'PasswordAuthentication no' >> /etc/ssh/sshd_config
echo 'PermitRootLogin without-password' >> /etc/ssh/sshd_config
echo 'PubkeyAuthentication yes' >> /etc/ssh/sshd_config
echo -n '${cloudrift_ssh_key.primary.public_key}' > /root/.ssh/authorized_keys
EOF
    )
  }
}

output "vm_id" {
  value = cloudrift_virtual_machine.machine0.id
}

output "public_ip" {
  value = cloudrift_virtual_machine.machine0.public_ip
}

output "status" {
  value = cloudrift_virtual_machine.machine0.status
}

output "port_mappings" {
  description = "Port mappings for shared-IP instances (null for dedicated IP)"
  value       = cloudrift_virtual_machine.machine0.port_mappings
}

output "virtual_machines" {
  value = cloudrift_virtual_machine.machine0.virtual_machines
}
