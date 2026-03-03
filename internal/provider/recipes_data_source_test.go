package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func Test_RecipesDataSource(t *testing.T) {
	t.Parallel()

	server := defaultHttpTestServer(nil)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read
			{
				Config: providerConfig(server.URL, "1.0") + `data "cloudrift_recipes" "default" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.cloudrift_recipes.default", "groups.0.recipes.0.name", "ubuntu"),
					resource.TestCheckResourceAttr("data.cloudrift_recipes.default", "groups.0.recipes.0.description", "Ubuntu 22.04 LTS"),
					resource.TestCheckResourceAttr("data.cloudrift_recipes.default", "groups.0.recipes.0.tags.#", "2"),
					resource.TestCheckResourceAttr("data.cloudrift_recipes.default", "groups.0.recipes.0.tags.0", "linux"),
					resource.TestCheckResourceAttr("data.cloudrift_recipes.default", "groups.0.recipes.0.tags.1", "ubuntu"),
					resource.TestCheckResourceAttr("data.cloudrift_recipes.default", "groups.0.recipes.1.name", "ubuntu-2"),
					resource.TestCheckResourceAttr("data.cloudrift_recipes.default", "groups.0.recipes.1.tags.#", "0"),
				),
			},
		},
	})
}
