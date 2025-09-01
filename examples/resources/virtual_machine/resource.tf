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
  public_key = trimspace(file("~/.ssh/id_ed25519.pub"))
}

resource "cloudrift_virtual_machine" "machine0" {
  recipe        = "Ubuntu 22.04 Server (NVidia)"
  datacenter    = "us-east-nc-nr-1"
  instance_type = "rtx59-16c-nr.1"
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

output "machine0_ip" {
  value = cloudrift_virtual_machine.machine0.public_ip
}

output "machine0" {
  value = cloudrift_virtual_machine.machine0.virtual_machines
}
