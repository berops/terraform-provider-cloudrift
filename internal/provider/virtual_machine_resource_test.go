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

	server := vmTestServerWithTeamCapture(keyName, publicKey, &capturedTeamID)

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
	server := vmTestServer(keyName, publicKey)

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

func vmTestServer(keyName, publicKey string) *httptest.Server {
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
		"/api/v1/instances/rent": func(w http.ResponseWriter, _ *http.Request) {
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
		"/api/v1/ssh-keys/add": func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodPost {
				var input struct {
					Data struct {
						Name      string `json:"name"`
						PublicKey string `json:"public_key"`
					} `json:"data"`
				}

				body, _ := io.ReadAll(req.Body)
				_ = json.Unmarshal(body, &input)

				resp := fmt.Sprintf(`
					{
						"data": {
							"public_key": {
								"id": "11111",
								"name": "%s",
								"public_key": "%s"
							}
						}
					}
				`, input.Data.Name, input.Data.PublicKey)

				w.Header().Set("Content-Type", "json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(resp))
				return
			}
		},
		"/api/v1/ssh-keys/list": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fmt.Appendf(nil, `
				{
					"data": {
						"keys": [
							{
								"id": "1",
								"name": "test-key",
								"public_key": "ssh-rsa AAAA testuser"
							},
							{
								"id": "11111",
								"name": "%s",
								"public_key": "%s"
							}
						]
					}
				}
			`, keyName, publicKey))
		},
		"/api/v1/ssh-keys/11111": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})
}

func providerConfigWithTeamID(baseURL, proto, teamID string) string {
	return fmt.Sprintf(`
provider "cloudrift" {
	base_url = "%s"
	proto_version = "%s"
	token = "test"
	team_id = "%s"
}
`, baseURL, proto, teamID)
}

func vmTestServerWithTeamCapture(keyName, publicKey string, capturedTeamID *string) *httptest.Server {
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
			body, _ := io.ReadAll(req.Body)
			var parsed struct {
				Data struct {
					TeamID *string `json:"team_id"`
				} `json:"data"`
			}
			_ = json.Unmarshal(body, &parsed)
			if parsed.Data.TeamID != nil {
				*capturedTeamID = *parsed.Data.TeamID
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
		"/api/v1/ssh-keys/add": func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodPost {
				var input struct {
					Data struct {
						Name      string `json:"name"`
						PublicKey string `json:"public_key"`
					} `json:"data"`
				}

				body, _ := io.ReadAll(req.Body)
				_ = json.Unmarshal(body, &input)

				resp := fmt.Sprintf(`
					{
						"data": {
							"public_key": {
								"id": "11111",
								"name": "%s",
								"public_key": "%s"
							}
						}
					}
				`, input.Data.Name, input.Data.PublicKey)

				w.Header().Set("Content-Type", "json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(resp))
				return
			}
		},
		"/api/v1/ssh-keys/list": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fmt.Appendf(nil, `
				{
					"data": {
						"keys": [
							{
								"id": "1",
								"name": "test-key",
								"public_key": "ssh-rsa AAAA testuser"
							},
							{
								"id": "11111",
								"name": "%s",
								"public_key": "%s"
							}
						]
					}
				}
			`, keyName, publicKey))
		},
		"/api/v1/ssh-keys/11111": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})
}
