package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func Test_VirtualMachineResource_TeamId(t *testing.T) {
	t.Parallel()

	keyName := "anotheruser-key"
	publicKey := "ssh-rsa AAAA anotheruser"
	teamID := "team-123"
	var capturedTeamID string

	server := newVMTestServer(keyName, publicKey, func(req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var parsed struct {
			Data struct {
				TeamID *string `json:"team_id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(body, &parsed)
		if parsed.Data.TeamID != nil {
			capturedTeamID = *parsed.Data.TeamID
		}
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfigWithTeamID(server.URL, "1.0", teamID) + fmt.Sprintf(`
					resource "cloudrift_ssh_key" "primary" {
					  name       = "%s"
					  public_key = "%s"
					}

					resource "cloudrift_virtual_machine" "machine0" {
					  recipe        = "ubuntu"
					  datacenter    = "us-east-nc-nr-1"
					  instance_type = "rtx49-10c-kn.1"
					  ssh_key_id    = cloudrift_ssh_key.primary.id
					}
				`, keyName, publicKey),
				Check: resource.TestCheckFunc(func(s *terraform.State) error {
					if capturedTeamID != teamID {
						return fmt.Errorf("expected team_id %q in rent request, got %q", teamID, capturedTeamID)
					}
					return nil
				}),
			},
		},
	})
}

func Test_VirtualMachineResrouce(t *testing.T) {
	t.Parallel()

	keyName := "anotheruser-key"
	publicKey := "ssh-rsa AAAA anotheruser"
	server := newVMTestServer(keyName, publicKey, nil)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL, "1.0") + fmt.Sprintf(`
					resource "cloudrift_ssh_key" "primary" {
					  name       = "%s"
					  public_key = "%s"
					}

					resource "cloudrift_virtual_machine" "machine0" {
					  recipe        = "ubuntu"
					  datacenter    = "us-east-nc-nr-1"
					  instance_type = "rtx49-10c-kn.1"
					  ssh_key_id    = cloudrift_ssh_key.primary.id
					}
				`, keyName, publicKey),
			},
		},
	})
}

// newVMTestServer creates a test server with instance, SSH key, and rent
// handlers. The optional onRent callback is invoked on each rent request
// before the response is written, allowing tests to inspect the request body.
func newVMTestServer(keyName, publicKey string, onRent func(req *http.Request)) *httptest.Server {
	status := "Active"
	instanceResponse := `
	{
		"data": {
			"instances": [
				{
					"id": "1",
					"node_id": "1",
					"node_mode": "Virtual Machine",
					"node_status": "Ready",
					"host_address": "127.0.0.1",
					"internal_host_address": "10.0.0.1",
					"resource_info": {
						"provider_name": "provider",
						"instance_type": "rtx49-10c-kn.1"
					},
					"virtual_machines": [
						{
							"vmid": 100,
							"name": "vm-1",
							"ready": true
						}
					],
					"status": "%s"
				}
			]
		}
	}
	`
	return defaultHttpTestServer(map[string]func(w http.ResponseWriter, req *http.Request){
		"/api/v1/instances/terminate": func(w http.ResponseWriter, _ *http.Request) {
			status = "Inactive"
			w.WriteHeader(http.StatusOK)
		},
		"/api/v1/instances/list": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fmt.Appendf(nil, instanceResponse, status))
		},
		"/api/v1/instances/rent": func(w http.ResponseWriter, req *http.Request) {
			if onRent != nil {
				onRent(req)
			}
			w.Header().Set("Content-Type", "json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`
				{
					"data": {
					 	"instance_ids": [
							"1"
						]
					}
				}
			`))
		},
		"/api/v1/ssh-keys/add":   sshKeyAddHandler(),
		"/api/v1/ssh-keys/list":  sshKeyListHandlerWithKey(keyName, publicKey),
		"/api/v1/ssh-keys/11111": sshKeyDeleteHandler(),
	})
}
