package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var (
	// testAccProtoV6ProviderFactories are used to instantiate a provider during
	// acceptance testing. The factory function will be invoked for every Terraform
	// CLI command executed to create a provider server to which the CLI can reattach.
	testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
		"cloudrift": providerserver.NewProtocol6WithError(New("test")()),
	}
)

// nolint
func providerConfig(baseURL, proto string) string {
	return fmt.Sprintf(`
provider "cloudrift" {
	base_url = "%s"
	proto_version = "%s"
	token = "test"
}
`, baseURL, proto)
}

func defaultHttpTestServer(handlers map[string]func(w http.ResponseWriter, req *http.Request)) *httptest.Server {
	if handlers == nil {
		handlers = make(map[string]func(w http.ResponseWriter, req *http.Request))
	}

	if _, ok := handlers["/api/v1/auth/me"]; !ok {
		handlers["/api/v1/auth/me"] = func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"email": "test@test.com"}}`))
		}
	}

	if _, ok := handlers["/api/v1/recipes/list"]; !ok {
		handlers["/api/v1/recipes/list"] = func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`
				{
					"data": {
						"groups": [
							{
							 "recipes": [
								{
									"name": "ubuntu",
									"description": "Ubuntu 22.04 LTS",
									"tags": ["linux", "ubuntu"],
									"details": {
										"VirtualMachine": {
											"cloudinit_url": "test",
											"image_url": "test"
										}
									}
								},
								{
									"name": "ubuntu-2",
									"description": "Ubuntu 24.04 LTS",
									"tags": [],
									"details": {
										"VirtualMachine": {
											"cloudinit_url": "test",
											"image_url": "test"
										}
									}
								}
							  ]
							}
						]
					}
				}
			`))
		}
	}

	if _, ok := handlers["/api/v1/instance-types/list"]; !ok {
		handlers["/api/v1/instance-types/list"] = func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`
				{
					"data": {
						"instance_types": [
							{
								"name": "test",
								"variants": [
								{
								 "name": "test-variant",
								 "cpu_count": 10,
								 "logical_cpu_count": 20,
								 "gpu_count": 1,
								 "disk": 1099511627776,
								 "dram": 68719476736,
								 "vram": 25769803776,
								 "cost_per_hour": 0.85,
								 "available_nodes": 5,
								 "available_nodes_per_dc": {"dc-1": 3, "dc-2": 2},
								 "nodes_per_dc": {"dc-1": 2, "dc-2": 1},
								 "ip_availability_per_dc": {"dc-1": {"public_ips": true}, "dc-2": {"public_ips": false}}
								}
								]
							}
						]
					}
				}
			`))
		}
	}

	if _, ok := handlers["/api/v1/ssh-keys/list"]; !ok {
		handlers["/api/v1/ssh-keys/list"] = func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`
				{
					"data": {
						"keys": [
							{
								"id": "1",
								"name": "test-key",
								"public_key": "ssh-rsa AAAA testuser"
							}
						]
					}
				}
			`))
		}
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler := handlers[strings.TrimSpace(r.URL.Path)]
		if handler == nil {
			panic("unsupported handler")
		}
		handler(w, r)
	}))
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

// sshKeyAddHandler returns a test handler for POST /api/v1/ssh-keys/add
// that echoes back the submitted key with a fixed ID of "11111".
func sshKeyAddHandler() func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			return
		}

		var input struct {
			Data struct {
				Name      string `json:"name"`
				PublicKey string `json:"public_key"`
			} `json:"data"`
		}

		body, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(body, &input)

		w.Header().Set("Content-Type", "json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(fmt.Appendf(nil, `
			{
				"data": {
					"public_key": {
						"id": "11111",
						"name": "%s",
						"public_key": "%s"
					}
				}
			}
		`, input.Data.Name, input.Data.PublicKey))
	}
}

// sshKeyListHandlerWithKey returns a test handler for GET /api/v1/ssh-keys/list
// that includes the default test key plus the given key with ID "11111".
func sshKeyListHandlerWithKey(keyName, publicKey string) func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
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
	}
}

// sshKeyDeleteHandler returns a test handler for DELETE /api/v1/ssh-keys/11111
// that responds with 200 OK.
func sshKeyDeleteHandler() func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}
