package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func Test_InstanceTypesDataSource(t *testing.T) {
	t.Parallel()

	server := defaultHttpTestServer(nil)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read
			{
				Config: providerConfig(server.URL, "1.0") + `data "cloudrift_instance_types" "default" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.cloudrift_instance_types.default", "instance_types.0.name", "test"),
					resource.TestCheckResourceAttr("data.cloudrift_instance_types.default", "instance_types.0.variants.0.name", "test-variant"),
				),
			},
		},
	})
}
