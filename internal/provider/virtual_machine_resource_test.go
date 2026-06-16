package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"

	"github.com/berops/terraform-provider-cloudrift/pkg/cloudriftapi"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// Test_VirtualMachineResource_FailsOnTerminalStatus covers every instance
// status that must abort Create and best-effort terminate the rented VM so it
// doesn't leak. Inactive is special: GetInstance converts it to ErrNotFound at
// the client level, so the provider reports a poll error rather than the status.
func Test_VirtualMachineResource_FailsOnTerminalStatus(t *testing.T) {
	t.Parallel()

	keyName := "anotheruser-key"
	publicKey := "ssh-rsa AAAA anotheruser"

	for _, tc := range []struct {
		status  string
		wantErr string
	}{
		{"Inactive", `failed to poll status`},
		{"Deactivating", `reached terminal status "Deactivating"`},
		{"Failed", `reached terminal status "Failed"`}, // server 0.59.0+
	} {
		t.Run(tc.status, func(t *testing.T) {
			t.Parallel()

			server, terminateCalls := newVMTestServerWithStatus(keyName, publicKey, tc.status, false)

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
						ExpectError: regexp.MustCompile(tc.wantErr),
					},
				},
			})

			if got := atomic.LoadInt32(terminateCalls); got < 1 {
				t.Fatalf("expected Create to call /instances/terminate at least once to release the failed VM, got %d calls", got)
			}
		})
	}
}

// newVMTestServerWithStatus creates a test server where the instance reports
// the given status and VM readiness. After terminate is called, the instance
// list returns empty so the test framework's destroy cleanup completes.
// The returned int32 pointer counts how many /instances/terminate calls the
// server has observed — used to verify best-effort cleanup on failed creates.
func newVMTestServerWithStatus(keyName, publicKey, status string, vmReady bool) (*httptest.Server, *int32) {
	var terminateCalls int32
	var terminated int32 // atomic: written by /terminate, read by /list concurrently

	instanceResponse := fmt.Sprintf(`
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
							"ready": %v
						}
					],
					"status": "%s"
				}
			]
		}
	}
	`, vmReady, status)

	server := defaultHttpTestServer(map[string]func(w http.ResponseWriter, req *http.Request){
		"/api/v1/instances/terminate": func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&terminateCalls, 1)
			atomic.StoreInt32(&terminated, 1)
			w.WriteHeader(http.StatusOK)
		},
		"/api/v1/instances/list": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "json")
			w.WriteHeader(http.StatusOK)
			if atomic.LoadInt32(&terminated) == 1 {
				_, _ = w.Write([]byte(`{"data": {"instances": []}}`))
				return
			}
			_, _ = w.Write([]byte(instanceResponse))
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
		"/api/v1/ssh-keys/add":   sshKeyAddHandler(),
		"/api/v1/ssh-keys/list":  sshKeyListHandlerWithKey(keyName, publicKey),
		"/api/v1/ssh-keys/11111": sshKeyDeleteHandler(),
	})

	return server, &terminateCalls
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

// Test_VirtualMachineResource_DeleteOnStuckDeactivating verifies that Delete
// treats an instance reporting Deactivating status as destroyed, rather than
// polling until the 5-minute timeout.
//
// Scenario: the VM provisions successfully (Active). The user runs terraform
// destroy. The backend accepts the terminate request and flips the instance
// into Deactivating, but — as seen in production when GPU capacity is tight —
// never completes the Deactivating→Inactive transition within the polling
// window. Before the fix, Delete would poll for 5 minutes and error with
// "Destruction timeout reached"; combined with the Create-side state-leak bug,
// this produced a permanent apply/destroy loop in consumers like Claudie.
func Test_VirtualMachineResource_DeleteOnStuckDeactivating(t *testing.T) {
	t.Parallel()

	keyName := "anotheruser-key"
	publicKey := "ssh-rsa AAAA anotheruser"
	server := newVMTestServerStuckDeactivating(keyName, publicKey)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Step 1 creates the VM (server reports Active+ready).
				// The framework's implicit destroy at the end of the test
				// exercises the Delete path against a backend that will
				// keep returning Deactivating forever after terminate —
				// this must still return success in well under 5 minutes.
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

// newVMTestServerStuckDeactivating starts as Active+ready (so Create
// succeeds), and after the first /instances/terminate call flips the instance
// to Deactivating and leaves it there indefinitely — never returning empty
// or Inactive. This simulates the CloudRift backend getting stuck mid-teardown
// when capacity is tight.
func newVMTestServerStuckDeactivating(keyName, publicKey string) *httptest.Server {
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
			// Simulate backend acknowledging the terminate request but
			// getting stuck: status flips to Deactivating and never
			// advances to Inactive.
			status = "Deactivating"
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
		"/api/v1/ssh-keys/add":   sshKeyAddHandler(),
		"/api/v1/ssh-keys/list":  sshKeyListHandlerWithKey(keyName, publicKey),
		"/api/v1/ssh-keys/11111": sshKeyDeleteHandler(),
	})
}

// Test_PopulateModelFromInstanceResponse_NullableFields guards the Plugin
// Framework contract: a Computed attribute that ends as Unknown after apply
// triggers "Provider returned invalid result object after apply" in OpenTofu.
// The function must assign Null in every nil branch.
func Test_PopulateModelFromInstanceResponse_NullableFields(t *testing.T) {
	t.Parallel()

	publicIP := "203.0.113.10"
	privateIP := "10.0.0.5"
	resourceInfo := &cloudriftapi.InstanceResourceInfo{
		ProviderName: "test-provider",
		InstanceType: "rtx49-10c-kn.1",
	}

	tests := []struct {
		name             string
		data             *cloudriftapi.InstanceAndUsageInfo
		wantPublicIP     types.String
		wantPrivateIP    types.String
		wantProviderName types.String
	}{
		{
			name: "all nullable fields populated",
			data: &cloudriftapi.InstanceAndUsageInfo{
				HostAddress:         &publicIP,
				InternalHostAddress: &privateIP,
				ResourceInfo:        resourceInfo,
			},
			wantPublicIP:     types.StringValue(publicIP),
			wantPrivateIP:    types.StringValue(privateIP),
			wantProviderName: types.StringValue("test-provider"),
		},
		{
			name: "host_address and internal_host_address nil",
			data: &cloudriftapi.InstanceAndUsageInfo{
				ResourceInfo: resourceInfo,
			},
			wantPublicIP:     types.StringNull(),
			wantPrivateIP:    types.StringNull(),
			wantProviderName: types.StringValue("test-provider"),
		},
		{
			name: "resource_info nil",
			data: &cloudriftapi.InstanceAndUsageInfo{
				HostAddress:         &publicIP,
				InternalHostAddress: &privateIP,
			},
			wantPublicIP:     types.StringValue(publicIP),
			wantPrivateIP:    types.StringValue(privateIP),
			wantProviderName: types.StringNull(),
		},
		{
			name:             "all three nullable fields nil — the regression case",
			data:             &cloudriftapi.InstanceAndUsageInfo{},
			wantPublicIP:     types.StringNull(),
			wantPrivateIP:    types.StringNull(),
			wantProviderName: types.StringNull(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var m virtualMachineModel
			diags := populateModelFromInstanceResponse(&m, tt.data)
			for _, d := range diags {
				if d.Severity() == diag.SeverityError {
					t.Fatalf("unexpected error diagnostic: %s — %s", d.Summary(), d.Detail())
				}
			}

			if !m.PublicIP.Equal(tt.wantPublicIP) {
				t.Errorf("PublicIP: got %v, want %v", m.PublicIP, tt.wantPublicIP)
			}
			if !m.PrivateIP.Equal(tt.wantPrivateIP) {
				t.Errorf("PrivateIP: got %v, want %v", m.PrivateIP, tt.wantPrivateIP)
			}
			if !m.ProviderName.Equal(tt.wantProviderName) {
				t.Errorf("ProviderName: got %v, want %v", m.ProviderName, tt.wantProviderName)
			}
		})
	}
}
