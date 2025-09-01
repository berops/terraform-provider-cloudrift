package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func Test_SSHKeyResource(t *testing.T) {
	t.Parallel()

	keyName := "anotheruser-key"
	publicKey := "ssh-rsa AAAA anotheruser"

	server := defaultHttpTestServer(map[string]func(w http.ResponseWriter, req *http.Request){
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

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL, "1.0") + fmt.Sprintf(`resource "cloudrift_ssh_key" "default" {
						name = "%s"
						public_key = "%s"
					}`, keyName, publicKey),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudrift_ssh_key.default", "name", keyName),
					resource.TestCheckResourceAttr("cloudrift_ssh_key.default", "id", "11111"),
					resource.TestCheckResourceAttr("cloudrift_ssh_key.default", "public_key", publicKey),
				),
			},
			{
				ResourceName:      "cloudrift_ssh_key.default",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
