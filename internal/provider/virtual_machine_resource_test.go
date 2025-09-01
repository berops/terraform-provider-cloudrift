package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

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
					  instance_type = "rtx49-8c-nr.1"
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
					"resource_info": {
						"provider_name": "provider",
						"instance_type": "rtx49-8c-nr.1"
					},
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
