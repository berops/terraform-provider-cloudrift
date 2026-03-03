package provider

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func Test_SSHKeyResource_TeamApiKeyError(t *testing.T) {
	t.Parallel()

	server := defaultHttpTestServer(map[string]func(w http.ResponseWriter, req *http.Request){
		"/api/v1/ssh-keys/add": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("User cannot be authenticated from the request"))
		},
		"/api/v1/ssh-keys/list": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"keys":[]}}`))
		},
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL, "1.0") + `resource "cloudrift_ssh_key" "default" {
					name = "test"
					public_key = "ssh-rsa AAAA test"
				}`,
				ExpectError: regexp.MustCompile("(?i)team API key"),
			},
		},
	})
}

func Test_SSHKeyResource(t *testing.T) {
	t.Parallel()

	keyName := "anotheruser-key"
	publicKey := "ssh-rsa AAAA anotheruser"

	server := defaultHttpTestServer(map[string]func(w http.ResponseWriter, req *http.Request){
		"/api/v1/ssh-keys/add":   sshKeyAddHandler(),
		"/api/v1/ssh-keys/list":  sshKeyListHandlerWithKey(keyName, publicKey),
		"/api/v1/ssh-keys/11111": sshKeyDeleteHandler(),
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
