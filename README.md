Terraform Provider - CloudRift
===

Terraform provider for [CloudRift](https://www.cloudrift.ai/)

## Example


To spin up a Virtual Machine with Ubuntu as the base image you can use the terraform provider to provision the infrastructure.

```
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
    name        = "primary"
    public_key = trimspace(file("~/.ssh/id_ed25519.pub"))
}

resource "cloudrift_virtual_machine" "machine0" {
    recipe          = "Ubuntu 22.04 Server (NVidia)"
    datacenter      = "us-east-nc-nr-1"
    instance_type   = "rtx49-8c-nr.1"
    ssh_key_id      = cloudrift_ssh_key.primary.id

    metadata = {
      # enable login as root with the provided key.
      startup_commands = base64encode("echo 'PermitRootLogin without-password' >> /etc/ssh/sshd_config && echo   'PubkeyAuthentication yes' >> /etc/ssh/sshd_config && (echo -n '${cloudrift_ssh_key.primary.public_key}' >   /root/.ssh/authorized_keys)")
    }
}

output "machine0_ip" {
    value = cloudrift_virtual_machine.machine0.public_ip
}

output "machine0" {
    value = cloudrift_virtual_machine.machine0.virtual_machines
}
```

After the Virtual Machine is created, you can SSH into it as root using the selected private key.
