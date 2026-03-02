package cloudriftapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// ErrNotFound is returned when the requested resource is not found.
//
// If the endpoint of the CloudRift server returns a HTTP cod 404 this
// will be interpreted as the ErrNotFound. Or the resource ById returns
// an empty response/Inactive resource.
var ErrNotFound = errors.New("resource not found")

const (
	Endpoint = "https://api.cloudrift.ai"
)

const (
	ProtoUpcoming = `~upcoming`
	Proto20250610 = "2025-06-10"
	Proto20250529 = "2025-05-29"
	Proto20250321 = "2025-03-21"
	Proto20250210 = "2025-02-10"
	Proto20240922 = "2024-09-22"
)

type HttpClientOption func(*HttpClient)

func WithRetryableHttpClient(retries int) HttpClientOption {
	return func(hc *HttpClient) {
		hc.retries = retries
	}
}

type AuthData struct {
	Token string
}

type HttpClient struct {
	HostURL      string
	HTTPClient   *http.Client
	auth         AuthData
	retries      int
	ProtoVersion string
	TeamID       string // Optional team ID for team-scoped operations.

	vmRecipies map[string]*RecipeDetails1
}

func NewCustom(endpoint, token, protoVersion, teamID string, opts ...HttpClientOption) (*HttpClient, error) {
	c := HttpClient{
		HostURL: endpoint,
		auth:    AuthData{Token: token},
		retries: 0,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		ProtoVersion: protoVersion,
		TeamID:       teamID,
		vmRecipies:   make(map[string]*RecipeDetails1),
	}

	for _, o := range opts {
		o(&c)
	}

	if endpoint == "" {
		c.HostURL = Endpoint
	}

	// ensure the server URL always has a trailing slash
	if !strings.HasSuffix(c.HostURL, "/") {
		c.HostURL += "/"
	}

	if protoVersion == "" {
		c.ProtoVersion = ProtoUpcoming
	}

	if err := c.Auth(); err != nil {
		return nil, fmt.Errorf("failed to authenticated: %w", err)
	}

	if err := c.refreshVMRecipeCache(); err != nil {
		return nil, fmt.Errorf("failed to refresh recipes cache: %w", err)
	}

	if len(c.vmRecipies) == 0 {
		return nil, errors.New("no recipes for VMs found")
	}

	return &c, nil
}

func (c *HttpClient) refreshVMRecipeCache() error {
	recipes, err := c.ListRecipes()
	if err != nil {
		return fmt.Errorf("failed to list recipes: %w", err)
	}

	if recipes == nil {
		return fmt.Errorf("invalid JSON response")
	}

	for _, group := range recipes.Data.Groups {
		for _, r := range group.Recipes {
			var vmDetails RecipeDetails1
			if err := json.Unmarshal(r.Details.union, &vmDetails); err != nil {
				continue
			}

			var empty RecipeDetails1
			if vmDetails != empty {
				name := strings.ToLower(r.Name)
				c.vmRecipies[name] = &vmDetails
			}
		}
	}

	return nil
}

func (c *HttpClient) findVMRecipe(recipe string) (*RecipeDetails1, error) {
	found := c.vmRecipies[recipe]
	if found == nil {
		if err := c.refreshVMRecipeCache(); err != nil {
			return nil, err
		}
		found = c.vmRecipies[recipe]
	}

	if found == nil {
		return nil, fmt.Errorf("recipe %s not found", recipe)
	}

	return found, nil
}

func (c *HttpClient) Auth() error {
	req, err := http.NewRequest(http.MethodPost, c.HostURL+"api/v1/auth/me", nil)
	if err != nil {
		return err
	}

	type auth struct {
		Data struct {
			Email string `json:"email"`
		} `json:"data"`
	}

	resp, err := DoRequestWithApiToken(c, req, func(resp *http.Response) (*auth, error) {
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		var data auth
		err = json.Unmarshal(b, &data)
		return &data, err
	})
	if err != nil {
		return err
	}

	if resp.Data.Email == "" {
		return errors.New("invalid api token")
	}
	return nil
}

func (c *HttpClient) ListRecipes() (*ListRecipesResponseProto, error) {
	req, err := NewListRecipesRequest(c.HostURL)
	if err != nil {
		return nil, err
	}

	resp, err := DoRequestWithApiToken(c, req, ParseListRecipesResponse)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.JSON200 == nil {
		return nil, errors.New(
			"listing recipes failed, expected response with code 200 which was not returned, but no error occurred with the request itself, most likely the API changed or the response has a missing 'Content-Type' for json",
		)
	}
	return resp.JSON200, nil
}

func DoRequestWithApiToken[Parsed any](c *HttpClient, req *http.Request, parse func(resp *http.Response) (*Parsed, error)) (*Parsed, error) {
	req.Header.Add("X-API-KEY", c.auth.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		// perform retries, if the client was configured as retryable.
		backoff := 1 * time.Second
		for retries := c.retries; retries > 0; retries-- {
			time.Sleep(backoff)
			backoff = backoff << 1
			resp, err = c.HTTPClient.Do(req)
			if err == nil {
				break
			}
		}
		// if the retries also failed, error out.
		if err != nil {
			return nil, fmt.Errorf("request failed: %w, the failed request was retried: %vx", err, c.retries)
		}
	}

	//nolint
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		if resp.StatusCode == http.StatusNotFound {
			return nil, ErrNotFound
		}

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("request %s failed: %s: body: %s", req.URL.String(), resp.Status, string(body))
	}

	return parse(resp)
}

func (c *HttpClient) AddSSHKey(name, publicKey string) (*GenerateSshKeyResponseProto, error) {
	body, err := marshalVersionedRequest(c.ProtoVersion, struct {
		Name      string  `json:"name"`
		PublicKey *string `json:"public_key"`
	}{Name: name, PublicKey: &publicKey})
	if err != nil {
		return nil, err
	}

	req, err := NewAddSshKeyRequestWithBody(c.HostURL, "application/json", body)
	if err != nil {
		return nil, err
	}

	resp, err := DoRequestWithApiToken(c, req, ParseAddSshKeyResponse)
	if err != nil {
		return nil, wrapSSHKeyAuthError(err)
	}
	if resp == nil || resp.JSON201 == nil {
		return nil, errors.New(
			"adding ssh-key failed, expected response with code 201 which was not returned, but no error occurred with the request itself, most likely the API changed, or the response has a missing 'Content-Type' for json",
		)
	}
	return resp.JSON201, nil
}

func (c *HttpClient) DeleteSSHKey(id string) error {
	req, err := NewDeleteSshKeyRequest(c.HostURL, id)
	if err != nil {
		return err
	}

	_, err = DoRequestWithApiToken(c, req, ParseDeleteSshKeyResponse)
	return wrapSSHKeyAuthError(err)
}

func (c *HttpClient) ListSSHKeys() ([]SshKey, error) {
	req, err := NewListSshKeysRequest(c.HostURL)
	if err != nil {
		return nil, err
	}

	resp, err := DoRequestWithApiToken(c, req, ParseListSshKeysResponse)
	if err != nil {
		return nil, wrapSSHKeyAuthError(err)
	}
	if resp == nil || resp.JSON200 == nil {
		return nil, errors.New(
			"listing ssh-keys failed, expected response with code 200 which was not returned, but no error occurred with the request itself, most likely the API changed, or the response has a missing 'Content-Type' for json",
		)
	}
	return resp.JSON200.Data.Keys, nil
}

func (c *HttpClient) TerminateInstance(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("empty instance id")
	}

	var selector InstancesSelector
	// Always terminate by specific instance ID to avoid accidentally
	// terminating other instances.
	if err := selector.FromInstancesSelector0(InstancesSelector0{ById: []string{id}}); err != nil {
		return err
	}

	body, err := marshalVersionedRequest(c.ProtoVersion, struct {
		Selector InstancesSelector `json:"selector"`
	}{Selector: selector})
	if err != nil {
		return err
	}

	req, err := NewTerminateInstancesRequestWithBody(c.HostURL, "application/json", body)
	if err != nil {
		return err
	}

	_, err = DoRequestWithApiToken(c, req, ParseTerminateInstancesResponse)
	return err
}

func (c *HttpClient) RentPublicInstanceVM(recipe, datacenter, instance, commands string, pubKeys []string) (*RentInstanceResponseProto, error) {
	if recipe == "" {
		return nil, errors.New("no image specified")
	}
	if len(pubKeys) == 0 || slices.Contains(pubKeys, "") {
		return nil, errors.New("no ssh key specified")
	}
	if datacenter == "" {
		return nil, errors.New("empty datacenter")
	}
	if instance == "" {
		return nil, errors.New("empty instance")
	}
	if commands != "" {
		raw := []byte(commands)
		l := base64.StdEncoding.DecodedLen(len(raw))
		dst := make([]byte, l)
		written, err := base64.StdEncoding.Decode(dst, raw)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 encoded startup commands: %w", err)
		}
		commands = string(dst[:written])
		commands = strings.TrimSpace(commands)
	}

	recipe = strings.ToLower(recipe)
	details, err := c.findVMRecipe(recipe)
	if err != nil {
		return nil, fmt.Errorf("failed to find the requested recipe %s: %w", recipe, err)
	}

	var keySelector InstanceSshKeySelector
	if err := keySelector.FromInstanceSshKeySelector1(InstanceSshKeySelector1{PublicKeys: pubKeys}); err != nil {
		return nil, err
	}

	var vmConfig InstanceConfiguration1
	vmConfig.VirtualMachine.CloudinitUrl = &details.VirtualMachine.CloudinitUrl
	vmConfig.VirtualMachine.ImageUrl = details.VirtualMachine.ImageUrl
	vmConfig.VirtualMachine.CloudinitCommands = &commands
	vmConfig.VirtualMachine.SshKey = &keySelector

	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)
	// seems like the API has problem parsing '>' thus don't escape htlm chars.
	// Also the reason why we don't use the auto-generated method for creating the config.
	enc.SetEscapeHTML(false)

	if err := enc.Encode(vmConfig); err != nil {
		return nil, err
	}

	instanceConfiguration := InstanceConfiguration{
		union: json.RawMessage(bytes.Clone(buf.Bytes())),
	}

	var nodeSelector NodeSelector0
	nodeSelector.ByInstanceTypeAndLocation.Datacenters = &[]string{datacenter}
	nodeSelector.ByInstanceTypeAndLocation.InstanceType = instance

	var instanceSelector NodeSelector
	if err := instanceSelector.FromNodeSelector0(nodeSelector); err != nil {
		return nil, err
	}

	var reqData RentInstanceRequestProto
	reqData.Data.Selector = instanceSelector
	reqData.Data.WithPublicIp = true
	reqData.Data.Config = instanceConfiguration
	if c.TeamID != "" {
		reqData.Data.TeamId = &c.TeamID
	}

	// Encode with "version" before "data" to match marshalVersionedRequest
	// convention. The generated RentInstanceRequestProto struct has Data
	// before Version, so we use an inline struct to control field order.
	// We reuse the same encoder to preserve SetEscapeHTML(false).
	buf.Reset()
	if err := enc.Encode(struct {
		Version string `json:"version"`
		Data    any    `json:"data"`
	}{Version: c.ProtoVersion, Data: reqData.Data}); err != nil {
		return nil, err
	}

	body := bytes.NewReader(buf.Bytes())
	req, err := NewRentInstanceRequestWithBody(c.HostURL, "application/json", body)
	if err != nil {
		return nil, err
	}

	resp, err := DoRequestWithApiToken(c, req, ParseRentInstanceResponse)
	if err != nil {
		return nil, err
	}

	if resp == nil || resp.JSON200 == nil {
		return nil, errors.New(
			"renting instance failed, expected response with code 200 which was not returned, but no error occurred with the request itself, most likely the API changed, or the response has a missing 'Content-Type' for json",
		)
	}

	return resp.JSON200, nil
}

func (c *HttpClient) listInstances(selector InstancesSelector) (*ListInstancesResponseProto, error) {
	body, err := marshalVersionedRequest(c.ProtoVersion, struct {
		Selector InstancesSelector `json:"selector"`
	}{Selector: selector})
	if err != nil {
		return nil, err
	}

	req, err := NewListInstancesRequestWithBody(c.HostURL, "application/json", body)
	if err != nil {
		return nil, err
	}

	resp, err := DoRequestWithApiToken(c, req, ParseListInstancesResponse)
	if err != nil {
		return nil, err
	}

	if resp == nil || resp.JSON200 == nil {
		return nil, errors.New(
			"listing instances failed, expected response with code 200 which was not returned, but no error occurred with the request itself, most likely the API changed, or the response has a missing 'Content-Type' for json",
		)
	}

	return resp.JSON200, nil
}

func (c *HttpClient) ListInstances() (*ListInstancesResponseProto, error) {
	var selector InstancesSelector
	statuses := StatusSelector{
		Statuses: []InstanceStatus{
			Active,
			Initializing,
			Deactivating,
		},
	}
	if c.TeamID != "" {
		scope, err := teamScope(c.TeamID)
		if err != nil {
			return nil, err
		}
		statuses.Scope = &scope
	}
	if err := selector.FromInstancesSelector1(InstancesSelector1{ByStatus: statuses}); err != nil {
		return nil, err
	}
	return c.listInstances(selector)
}

func (c *HttpClient) GetInstance(id string) (*InstanceAndUsageInfo, error) {
	var selector InstancesSelector
	if c.TeamID != "" {
		// Team instances are not visible via ById; query by team scope and filter.
		statuses := StatusSelector{
			Statuses: []InstanceStatus{Active, Initializing, Deactivating},
		}
		scope, err := teamScope(c.TeamID)
		if err != nil {
			return nil, err
		}
		statuses.Scope = &scope
		if err := selector.FromInstancesSelector1(InstancesSelector1{ByStatus: statuses}); err != nil {
			return nil, err
		}
	} else {
		if err := selector.FromInstancesSelector0(InstancesSelector0{ById: []string{id}}); err != nil {
			return nil, err
		}
	}

	instances, err := c.listInstances(selector)
	if err != nil {
		return nil, err
	}

	for _, i := range instances.Data.Instances {
		if i.Id == id {
			if i.Status == Inactive {
				// listing ById will return the Instance
				// thus, check if still active.
				return nil, ErrNotFound
			}
			return &i, nil
		}
	}

	return nil, ErrNotFound
}

// teamScope returns a SelectorScope that targets the given team.
func teamScope(teamID string) (SelectorScope, error) {
	var scope SelectorScope
	err := scope.FromSelectorScope1(SelectorScope1{Teams: []string{teamID}})
	return scope, err
}

// marshalVersionedRequest serializes a request body with "version" before "data".
// The CloudRift API parses JSON sequentially and requires "version" first.
func marshalVersionedRequest(version string, data any) (io.Reader, error) {
	b, err := json.Marshal(struct {
		Version string `json:"version"`
		Data    any    `json:"data"`
	}{Version: version, Data: data})
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

// wrapSSHKeyAuthError detects 401 errors on SSH key endpoints and provides
// an actionable error message about team API key limitations.
func wrapSSHKeyAuthError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "401") {
		return fmt.Errorf("%w — SSH key operations require a personal API key. "+
			"Team API keys cannot manage SSH keys (by design, for security auditability). "+
			"Use a service account: create a dedicated user, add it to your team, "+
			"and use that user's personal API key in the provider configuration", err)
	}
	return err
}

func (c *HttpClient) ListInstanceTypes() (*ListInstanceTypesResponseProto, error) {
	var all InstanceTypeSelector
	if err := all.FromInstanceTypeSelector0(InstanceTypeSelector0All); err != nil {
		return nil, err
	}

	body, err := marshalVersionedRequest(c.ProtoVersion, struct {
		Selector *InstanceTypeSelector `json:"selector,omitempty"`
	}{Selector: &all})
	if err != nil {
		return nil, err
	}

	req, err := NewListInstanceTypesRequestWithBody(c.HostURL, "application/json", body)
	if err != nil {
		return nil, err
	}

	resp, err := DoRequestWithApiToken(c, req, ParseListInstanceTypesResponse)
	if err != nil {
		return nil, err
	}

	if resp == nil || resp.JSON200 == nil {
		return nil, errors.New(
			"listing instance-types failed, expected response with code 200 which was not returned, but no error occurred with the request itself, most likely the API changed, or the response has a missing 'Content-Type' for json",
		)
	}

	return resp.JSON200, nil
}
