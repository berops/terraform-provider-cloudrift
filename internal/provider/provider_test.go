package provider

import (
	"fmt"
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
									"details": {
										"VirtualMachine": {
											"cloudinit_url": "test",
											"image_url": "test"
										}
									}
								},
								{
									"name": "ubuntu-2",
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
								 "name": "test-variant"
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
			panic("unsuported handler")
		}
		handler(w, r)
	}))
}
