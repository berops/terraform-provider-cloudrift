package provider

import (
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/berops/terraform-provider-cloudrift/pkg/cloudriftapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAccPreCheck skips tests if CLOUDRIFT_TOKEN is not set.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("CLOUDRIFT_TOKEN") == "" {
		t.Skip("CLOUDRIFT_TOKEN must be set for acceptance tests")
	}
}

// cheapestInstance holds the cheapest available instance type found via the API.
type cheapestInstance struct {
	variantName string
	datacenter  string
	costPerHour float64
}

// findCheapestAvailableInstance queries the CloudRift API and returns the
// cheapest instance variant that has at least one available node.
func findCheapestAvailableInstance(t *testing.T) cheapestInstance {
	t.Helper()

	token := os.Getenv("CLOUDRIFT_TOKEN")
	baseURL := os.Getenv("CLOUDRIFT_BASE_URL")
	teamID := os.Getenv("CLOUDRIFT_TEAM_ID")

	client, err := cloudriftapi.NewCustom(baseURL, token, cloudriftapi.ProtoUpcoming, teamID)
	if err != nil {
		t.Fatalf("failed to create CloudRift client: %v", err)
	}

	resp, err := client.ListInstanceTypes()
	if err != nil {
		t.Fatalf("failed to list instance types: %v", err)
	}

	best := cheapestInstance{costPerHour: math.MaxFloat64}

	for _, it := range resp.Data.InstanceTypes {
		for _, v := range it.Variants {
			if v.AvailableNodes <= 0 || v.CostPerHour <= 0 {
				continue
			}
			if v.CostPerHour >= best.costPerHour {
				continue
			}
			// Find a datacenter with availability for this variant.
			for dc, count := range v.AvailableNodesPerDc {
				if count > 0 {
					best = cheapestInstance{
						variantName: v.Name,
						datacenter:  dc,
						costPerHour: v.CostPerHour,
					}
					break
				}
			}
		}
	}

	if best.variantName == "" {
		t.Fatal("no available instance types found")
	}

	t.Logf("cheapest available instance: %s in %s ($%.4f/hr)", best.variantName, best.datacenter, best.costPerHour)
	return best
}

func TestAcc_InstanceTypesDataSource(t *testing.T) {
	testAccPreCheck(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
					provider "cloudrift" {}
					data "cloudrift_instance_types" "all" {}
				`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.cloudrift_instance_types.all", "instance_types.#"),
				),
			},
		},
	})
}

func TestAcc_RecipesDataSource(t *testing.T) {
	testAccPreCheck(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
					provider "cloudrift" {}
					data "cloudrift_recipes" "all" {}
				`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.cloudrift_recipes.all", "groups.#"),
				),
			},
		},
	})
}

func TestAcc_SSHKeyResource(t *testing.T) {
	testAccPreCheck(t)

	sshPublicKey := os.Getenv("CLOUDRIFT_TEST_SSH_PUBLIC_KEY")
	if sshPublicKey == "" {
		t.Skip("CLOUDRIFT_TEST_SSH_PUBLIC_KEY must be set")
	}

	keyName := fmt.Sprintf("ci-test-%d", time.Now().UnixNano())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					provider "cloudrift" {}

					resource "cloudrift_ssh_key" "test" {
						name       = %q
						public_key = %q
					}

					data "cloudrift_ssh_key" "lookup" {
						name = cloudrift_ssh_key.test.name
					}
				`, keyName, sshPublicKey),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudrift_ssh_key.test", "name", keyName),
					resource.TestCheckResourceAttrSet("cloudrift_ssh_key.test", "id"),
					resource.TestCheckResourceAttrPair(
						"data.cloudrift_ssh_key.lookup", "id",
						"cloudrift_ssh_key.test", "id",
					),
				),
			},
		},
	})
}

func TestAcc_VirtualMachineResource(t *testing.T) {
	testAccPreCheck(t)

	if os.Getenv("CLOUDRIFT_TEAM_ID") == "" {
		t.Skip("CLOUDRIFT_TEAM_ID must be set for VM tests")
	}

	sshPublicKey := os.Getenv("CLOUDRIFT_TEST_SSH_PUBLIC_KEY")
	if sshPublicKey == "" {
		t.Skip("CLOUDRIFT_TEST_SSH_PUBLIC_KEY must be set")
	}

	instance := findCheapestAvailableInstance(t)

	keyName := fmt.Sprintf("ci-test-vm-%d", time.Now().UnixNano())
	recipe := "Ubuntu 22.04 Server (NVidia)"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					provider "cloudrift" {}

					resource "cloudrift_ssh_key" "test" {
						name       = %q
						public_key = %q
					}

					resource "cloudrift_virtual_machine" "test" {
						recipe        = %q
						datacenter    = %q
						instance_type = %q
						ssh_key_id    = cloudrift_ssh_key.test.id
					}
				`, keyName, sshPublicKey, recipe, instance.datacenter, instance.variantName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("cloudrift_virtual_machine.test", "id"),
					resource.TestCheckResourceAttrSet("cloudrift_virtual_machine.test", "public_ip"),
					resource.TestCheckResourceAttr("cloudrift_virtual_machine.test", "status", "Active"),
				),
			},
		},
	})
}
