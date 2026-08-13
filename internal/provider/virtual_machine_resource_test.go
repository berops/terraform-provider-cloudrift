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

// Test_VirtualMachineResource_ImageUrl verifies that a recipe holding a URL
// sends that URL straight through to the rent request, bypassing the recipe
// catalog lookup (whose default test image_url is "test").
func Test_VirtualMachineResource_ImageUrl(t *testing.T) {
	t.Parallel()

	keyName := "anotheruser-key"
	publicKey := "ssh-rsa AAAA anotheruser"
	// Mixed case on purpose: URL paths are case-sensitive, unlike recipe names.
	customImageURL := "https://example.com/Custom-Image.IMG"
	var capturedImageURL string

	server := newVMTestServer(keyName, publicKey, func(req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var parsed struct {
			Data struct {
				Config struct {
					VirtualMachine struct {
						ImageUrl string `json:"image_url"`
					} `json:"VirtualMachine"`
				} `json:"config"`
			} `json:"data"`
		}
		_ = json.Unmarshal(body, &parsed)
		capturedImageURL = parsed.Data.Config.VirtualMachine.ImageUrl
	})

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
					  recipe        = "%s"
					  datacenter    = "us-east-nc-nr-1"
					  instance_type = "rtx49-10c-kn.1"
					  ssh_key_id    = cloudrift_ssh_key.primary.id
					}
				`, keyName, publicKey, customImageURL),
				Check: resource.TestCheckFunc(func(s *terraform.State) error {
					if capturedImageURL != customImageURL {
						return fmt.Errorf("expected image_url %q in rent request, got %q", customImageURL, capturedImageURL)
					}
					return nil
				}),
			},
		},
	})
}

// Test_VirtualMachineResource_RecipeRequired verifies that a missing or empty
// recipe is rejected at plan time.
func Test_VirtualMachineResource_RecipeRequired(t *testing.T) {
	t.Parallel()

	keyName := "anotheruser-key"
	publicKey := "ssh-rsa AAAA anotheruser"
	server := newVMTestServer(keyName, publicKey, nil)

	testCases := []struct {
		name    string
		extra   string
		errorRe string
	}{
		{name: "not set", extra: "", errorRe: `(?i)"recipe" is required`},
		{name: "empty string", extra: `recipe = ""`, errorRe: `(?i)"recipe" must not be empty`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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
							  %s
							  datacenter    = "us-east-nc-nr-1"
							  instance_type = "rtx49-10c-kn.1"
							  ssh_key_id    = cloudrift_ssh_key.primary.id
							}
						`, keyName, publicKey, tc.extra),
						ExpectError: regexp.MustCompile(tc.errorRe),
					},
				},
			})
		})
	}
}

// A freshly rented VM can be missing from the first poll (eventual consistency)
// or briefly Inactive. Create must keep polling through that not-found rather
// than hard-failing on it.
func Test_VirtualMachineResource_ToleratesTransientNotFound(t *testing.T) {
	t.Parallel()

	keyName := "anotheruser-key"
	publicKey := "ssh-rsa AAAA anotheruser"

	activeResponse := `
	{
		"data": {
			"instances": [
				{
					"id": "1",
					"node_id": "1",
					"node_mode": "Virtual Machine",
					"node_status": "Ready",
					"host_address": "127.0.0.1",
					"virtual_machines": [{"vmid": 100, "name": "vm-1", "ready": true}],
					"status": "Active"
				}
			]
		}
	}`

	var listCalls, terminated int32
	server := defaultHttpTestServer(map[string]func(w http.ResponseWriter, req *http.Request){
		"/api/v1/instances/list": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// After terminate (destroy step), report gone so Delete converges.
			// Otherwise: first poll not listed yet, subsequent polls Active.
			if atomic.LoadInt32(&terminated) == 1 || atomic.AddInt32(&listCalls, 1) <= 1 {
				_, _ = w.Write([]byte(`{"data":{"instances":[]}}`))
				return
			}
			_, _ = w.Write([]byte(activeResponse))
		},
		"/api/v1/instances/terminate": func(w http.ResponseWriter, _ *http.Request) {
			atomic.StoreInt32(&terminated, 1)
			w.WriteHeader(http.StatusOK)
		},
		"/api/v1/instances/rent": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"instance_ids":["1"]}}`))
		},
		"/api/v1/ssh-keys/add":   sshKeyAddHandler(),
		"/api/v1/ssh-keys/list":  sshKeyListHandlerWithKey(keyName, publicKey),
		"/api/v1/ssh-keys/11111": sshKeyDeleteHandler(),
	})
	defer server.Close()

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
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudrift_virtual_machine.machine0", "id", "1"),
					resource.TestCheckResourceAttr("cloudrift_virtual_machine.machine0", "status", "Active"),
				),
			},
		},
	})
}

func Test_VirtualMachineResource_Name(t *testing.T) {
	t.Parallel()

	keyName := "anotheruser-key"
	publicKey := "ssh-rsa AAAA anotheruser"
	vmName := "mycluster-a3f2-pool1-01"
	var capturedName string

	server := newVMTestServer(keyName, publicKey, func(req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var parsed struct {
			Data struct {
				Name *string `json:"name"`
			} `json:"data"`
		}
		_ = json.Unmarshal(body, &parsed)
		if parsed.Data.Name != nil {
			capturedName = *parsed.Data.Name
		}
	})

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
					  name          = "%s"
					  recipe        = "ubuntu"
					  datacenter    = "us-east-nc-nr-1"
					  instance_type = "rtx49-10c-kn.1"
					  ssh_key_id    = cloudrift_ssh_key.primary.id
					}
				`, keyName, publicKey, vmName),
				Check: resource.TestCheckFunc(func(s *terraform.State) error {
					if capturedName != vmName {
						return fmt.Errorf("expected name %q in rent request, got %q", vmName, capturedName)
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
// doesn't leak. A freshly rented VM that is listed as Inactive has failed to
// come up, so Create treats it as terminal (a not-yet-listed id, by contrast,
// keeps polling — see Test_VirtualMachineResource_ToleratesTransientNotFound).
func Test_VirtualMachineResource_FailsOnTerminalStatus(t *testing.T) {
	t.Parallel()

	keyName := "anotheruser-key"
	publicKey := "ssh-rsa AAAA anotheruser"

	for _, tc := range []struct {
		status  string
		wantErr string
	}{
		{"Inactive", `reached terminal status "Inactive"`},
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

// Test_PopulateModelFromInstanceResponse_LoginInfoVariants guards username
// extraction across all three InstanceLoginInfo variants. Since API v061,
// /instances/list defaults with_credentials=false and returns the
// HiddenPassword variant, which the provider must still read the username from.
func Test_PopulateModelFromInstanceResponse_LoginInfoVariants(t *testing.T) {
	t.Parallel()

	usernameAndPassword := func() *cloudriftapi.InstanceLoginInfo {
		var li cloudriftapi.InstanceLoginInfo
		var v cloudriftapi.InstanceLoginInfo0
		v.UsernameAndPassword.Username = "up-user"
		v.UsernameAndPassword.Password = "secret"
		if err := li.FromInstanceLoginInfo0(v); err != nil {
			t.Fatal(err)
		}
		return &li
	}
	usernameOnly := func() *cloudriftapi.InstanceLoginInfo {
		var li cloudriftapi.InstanceLoginInfo
		var v cloudriftapi.InstanceLoginInfo1
		v.Username.Username = "ssh-user"
		if err := li.FromInstanceLoginInfo1(v); err != nil {
			t.Fatal(err)
		}
		return &li
	}
	hiddenPassword := func() *cloudriftapi.InstanceLoginInfo {
		var li cloudriftapi.InstanceLoginInfo
		var v cloudriftapi.InstanceLoginInfo2
		v.HiddenPassword.Username = "hidden-user"
		if err := li.FromInstanceLoginInfo2(v); err != nil {
			t.Fatal(err)
		}
		return &li
	}

	tests := []struct {
		name      string
		loginInfo *cloudriftapi.InstanceLoginInfo
		want      string
	}{
		{"UsernameAndPassword", usernameAndPassword(), "up-user"},
		{"Username (SSH-key only)", usernameOnly(), "ssh-user"},
		{"HiddenPassword (v061 default)", hiddenPassword(), "hidden-user"},
		{"nil login_info", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := &cloudriftapi.InstanceAndUsageInfo{
				VirtualMachines: []cloudriftapi.InstanceVirtualMachineInfo{
					{Vmid: 1, Name: "vm-1", LoginInfo: tt.loginInfo},
				},
			}

			var m virtualMachineModel
			diags := populateModelFromInstanceResponse(&m, data)
			for _, d := range diags {
				if d.Severity() == diag.SeverityError {
					t.Fatalf("unexpected error diagnostic: %s — %s", d.Summary(), d.Detail())
				}
			}

			elems := m.VirtualMachines.Elements()
			if len(elems) != 1 {
				t.Fatalf("VirtualMachines: got %d elements, want 1", len(elems))
			}
			obj, ok := elems[0].(types.Object)
			if !ok {
				t.Fatalf("VirtualMachines[0]: got %T, want types.Object", elems[0])
			}
			username := obj.Attributes()["username"]
			want := types.StringNull()
			if tt.want != "" {
				want = types.StringValue(tt.want)
			}
			if !username.Equal(want) {
				t.Errorf("username: got %v, want %v", username, want)
			}
		})
	}
}
