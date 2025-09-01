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

	vmRecipies map[string]*RecipeDetails1
}

func NewCustom(endpoint, token, protoVersion string, opts ...HttpClientOption) (*HttpClient, error) {
	c := HttpClient{
		HostURL: endpoint,
		auth:    AuthData{Token: token},
		retries: 0,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		ProtoVersion: protoVersion,
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
		c.ProtoVersion = Proto20250610
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
	var reqData AddSshKeyRequestProto
	reqData.Version = c.ProtoVersion
	reqData.Data.Name = name
	reqData.Data.PublicKey = &publicKey

	req, err := NewAddSshKeyRequest(c.HostURL, reqData)
	if err != nil {
		return nil, err
	}

	resp, err := DoRequestWithApiToken(c, req, ParseAddSshKeyResponse)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.JSON201 == nil {
		return nil, errors.New(
			"listing ssh-keys failed, expected response with code 201 which was not returned, but no error occurred with the request itself, most likely the API changed, or the response has a missing 'Content-Type' for json",
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
	return err
}

func (c *HttpClient) ListSSHKeys() ([]SshKey, error) {
	req, err := NewListSshKeysRequest(c.HostURL)
	if err != nil {
		return nil, err
	}

	resp, err := DoRequestWithApiToken(c, req, ParseListSshKeysResponse)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.JSON200 == nil {
		return nil, errors.New(
			"listing ssh-keys failed, expected response with code 200 which was not returned, but no error occurred with the request itself, most likely the API changed, or the response has a missing 'Content-Type' for json",
		)
	}
	return resp.JSON200.Data.Keys, nil
}

func (c *HttpClient) TerminateInstance(id string) error {
	var selector InstancesSelector
	if err := selector.FromInstancesSelector0(InstancesSelector0{ById: []string{id}}); err != nil {
		return err
	}

	var reqData TerminateInstancesRequestProto
	reqData.Version = c.ProtoVersion
	reqData.Data.Selector = selector

	req, err := NewTerminateInstancesRequest(c.HostURL, reqData)
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
		union: json.RawMessage(buf.Bytes()),
	}

	var nodeSelector NodeSelector0
	nodeSelector.ByInstanceTypeAndLocation.Datacenters = &[]string{datacenter}
	nodeSelector.ByInstanceTypeAndLocation.InstanceType = instance

	var instanceSelector NodeSelector
	if err := instanceSelector.FromNodeSelector0(nodeSelector); err != nil {
		return nil, err
	}

	var reqData RentInstanceRequestProto
	reqData.Version = c.ProtoVersion
	reqData.Data.Selector = instanceSelector
	reqData.Data.WithPublicIp = true
	reqData.Data.Config = instanceConfiguration

	buf.Reset()
	if err := enc.Encode(reqData); err != nil {
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
	var listRequest ListInstancesRequestProto
	listRequest.Data.Selector = selector
	listRequest.Version = c.ProtoVersion

	req, err := NewListInstancesRequest(c.HostURL, listRequest)
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
	err := selector.FromInstancesSelector1(InstancesSelector1{
		ByStatus: []InstanceStatus{
			Active,
			Initializing,
			Deactivating,
			// Inactive, We don't want to include inactive VMs in the response.
		},
	})
	if err != nil {
		return nil, err
	}
	return c.listInstances(selector)
}

func (c *HttpClient) GetInstance(id string) (*InstanceAndUsageInfo, error) {
	var selector InstancesSelector
	if err := selector.FromInstancesSelector0(InstancesSelector0{ById: []string{id}}); err != nil {
		return nil, err
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

func (c *HttpClient) ListInstanceTypes() (*ListInstanceTypesResponseProto, error) {
	var all InstanceTypeSelector
	if err := all.FromInstanceTypeSelector0(InstanceTypeSelector0All); err != nil {
		return nil, err
	}

	var listRequest ListInstanceTypesRequestProto
	listRequest.Version = c.ProtoVersion
	listRequest.Data.Selector = &all

	req, err := NewListInstanceTypesRequest(c.HostURL, listRequest)
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
