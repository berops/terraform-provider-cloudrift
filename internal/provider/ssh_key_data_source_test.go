package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func Test_SSHKeyDataSource(t *testing.T) {
	t.Parallel()

	server := defaultHttpTestServer(nil)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read
			{
				Config: providerConfig(server.URL, "1.0") + `data "cloudrift_ssh_key" "default" {
					name = "test-key"
				}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.cloudrift_ssh_key.default", "name", "test-key"),
					resource.TestCheckResourceAttr("data.cloudrift_ssh_key.default", "id", "1"),
					resource.TestCheckResourceAttr("data.cloudrift_ssh_key.default", "public_key", "ssh-rsa AAAA testuser"),
				),
			},
		},
	})
}
