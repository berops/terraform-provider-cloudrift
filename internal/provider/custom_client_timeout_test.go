package provider

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/berops/terraform-provider-cloudrift/pkg/cloudriftapi"
)

// slowInstanceListServer returns a test server whose /instances/list handler
// sleeps for `delay` before responding with a single Active instance (id "1").
// Auth/recipes handlers stay fast so NewCustom construction succeeds.
func slowInstanceListServer(delay time.Duration) *httptest.Server {
	return defaultHttpTestServer(map[string]func(w http.ResponseWriter, req *http.Request){
		"/api/v1/instances/list": func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(delay)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"instances":[{"id":"1","node_id":"1","node_mode":"Virtual Machine","node_status":"Ready","status":"Active"}]}}`))
		},
	})
}

// A generous timeout lets a slow /instances/list complete: GetInstance returns
// the instance instead of hard-failing (the reported wedge).
func Test_GetInstance_SlowResponse_SucceedsWithTimeout(t *testing.T) {
	server := slowInstanceListServer(50 * time.Millisecond)
	defer server.Close()

	client, err := cloudriftapi.NewCustom(server.URL, "test", "", "", cloudriftapi.WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("NewCustom: %v", err)
	}

	vm, err := client.GetInstance("1")
	if err != nil {
		t.Fatalf("GetInstance with generous timeout should succeed, got: %v", err)
	}
	if vm.Id != "1" {
		t.Fatalf("expected instance id 1, got %q", vm.Id)
	}
}

// A too-short timeout still surfaces an honest error — it must NOT be swallowed
// or misread as not-found (which would drop the resource from state and leak a
// recreate).
func Test_GetInstance_SlowResponse_ErrorsWhenTimeoutTooShort(t *testing.T) {
	server := slowInstanceListServer(150 * time.Millisecond)
	defer server.Close()

	client, err := cloudriftapi.NewCustom(server.URL, "test", "", "", cloudriftapi.WithTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewCustom: %v", err)
	}

	_, err = client.GetInstance("1")
	if err == nil {
		t.Fatal("GetInstance should error when the timeout is shorter than the response")
	}
	if errors.Is(err, cloudriftapi.ErrNotFound) {
		t.Fatal("a timeout must not be reported as ErrNotFound")
	}
}
