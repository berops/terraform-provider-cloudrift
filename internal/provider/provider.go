package provider

import (
	"context"
	"os"

	"github.com/berops/terraform-provider-cloudrift/pkg/cloudriftapi"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ provider.Provider                       = &CloudRiftProvider{}
	_ provider.ProviderWithFunctions          = &CloudRiftProvider{}
	_ provider.ProviderWithEphemeralResources = &CloudRiftProvider{}
)

type CloudRiftProviderModel struct {
	// X-API-Token used for interacting with the CloudRift platform API.
	Token types.String `tfsdk:"token"`

	// Optional URL configurable at the provider level, if not set the default
	// URL from the openapispec will be used.
	BaseURL types.String `tfsdk:"base_url"`

	// Optional Protocol Version configurable at the provider level, if not set the
	// default protocol version will be used.
	ProtoVersion types.String `tfsdk:"proto_version"`
}

type CloudRiftProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

func (p *CloudRiftProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "cloudrift"
	resp.Version = p.version
}

func (p *CloudRiftProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "Interact with CloudRift platform.",
		MarkdownDescription: "Interact with CloudRift platform",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				Description:         "Token for CloudRift platform API. May also be provided via CLOUDRIFT_TOKEN environment variable.",
				MarkdownDescription: "Token for CloudRift platform API. May also be provided via CLOUDRIFT_TOKEN environment variable.",
				Sensitive:           true,
				Optional:            true, // can be fetched from env.
			},
			"base_url": schema.StringAttribute{
				Description: "Base URL for the CloudRift platform API. If not specified the provider has a built in default Base URL that will be used." +
					"May also be provided via CLOUDRIFT_BASE_URL environment variable.",
				MarkdownDescription: "Base URL for the CloudRift platform API. If not specified the provider has a built in default Base URL that will be used." +
					"May also be provided via CLOUDRIFT_BASE_URL environment variable.",
				Optional: true, // can be fetched from env.
			},
			"proto_version": schema.StringAttribute{
				Description: "Protocol Version to be used for the CloudRift platform API." +
					"If not specified the provider has a built in default version that will be used. May also be provided via CLOUDRIFT_PROTO_VERSION environment variable.",
				MarkdownDescription: "Protocol Version to be used for the CloudRift platform API." +
					"If not specified the provider has a built in default version that will be used. May also be provided via CLOUDRIFT_PROTO_VERSION environment variable.",
				Optional: true, // can be fetched from env.
			},
		},
	}
}

func (p *CloudRiftProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config CloudRiftProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Unknown CloudRift API Token",
			"The provider cannot create the CloudRift API client as there is an unknown configuration for the CloudRift API token."+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the CLOUDRIFT_TOKEN environment variable.",
		)
	}

	if config.BaseURL.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("base_url"),
			"Unknown CloudRift Base URL",
			"The provider cannot create the CloudRift API client as there is an unknown configuration for the CloudRift API Base URL."+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the CLOUDRIFT_BASE_URL environment variable.",
		)
	}

	if config.ProtoVersion.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("proto_version"),
			"Unknown CloudRift Protocol Version",
			"The provider cannot create the CloudRift API client as there is an unknown configuration for the CloudRift API Protocol Version."+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the CLOUDRIFT_PROTO_VERSION environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	token := os.Getenv("CLOUDRIFT_TOKEN")
	baseURL := os.Getenv("CLOUDRIFT_BASE_URL")
	protoVersion := os.Getenv("CLOUDRIFT_PROTO_VERSION")

	if !config.Token.IsNull() {
		token = config.Token.ValueString()
	}

	if !config.BaseURL.IsNull() {
		baseURL = config.BaseURL.ValueString()
	}

	if !config.ProtoVersion.IsNull() {
		protoVersion = config.ProtoVersion.ValueString()
	}

	if baseURL == "" {
		baseURL = cloudriftapi.Endpoint
	}

	if protoVersion == "" {
		protoVersion = cloudriftapi.Proto20250610
	}

	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing CloudRift API Token",
			"The provider cannot create the CloudRift API client as there is a missing or empty value for the CloudRift API token."+
				"Set the token value in the configuration or use the CLOUDRIFT_TOKEN environment variable."+
				"If either is already set, ensure the value is not empty.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	client, err := cloudriftapi.NewCustom(baseURL, token, protoVersion, cloudriftapi.WithRetryableHttpClient(4))
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create CloudRift API Client",
			"An unexpected error occurred when creating the CloudRift API client."+
				"If the error is not clear, please contact the provider developers.\n\n"+
				"CloudRift Client Error: "+err.Error(),
		)
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *CloudRiftProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewSSHKeyResource,
		NewInstanceResource,
	}
}

func (p *CloudRiftProvider) EphemeralResources(ctx context.Context) []func() ephemeral.EphemeralResource {
	return []func() ephemeral.EphemeralResource{}
}

func (p *CloudRiftProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewSSHKeyDataSource,
		NewRecipesDataSource,
		NewInstanceTypesDataSource,
	}
}

func (p *CloudRiftProvider) Functions(ctx context.Context) []func() function.Function {
	return []func() function.Function{}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &CloudRiftProvider{
			version: version,
		}
	}
}
