package provider

import (
	"context"
	"fmt"

	"github.com/berops/terraform-provider-cloudrift/pkg/cloudriftapi"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &sshKeyDataSource{}
	_ datasource.DataSourceWithConfigure = &sshKeyDataSource{}
)

type sshKeyDataModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	PublicKey types.String `tfsdk:"public_key"`
}

type sshKeyDataSource struct {
	client *cloudriftapi.HttpClient
}

func NewSSHKeyDataSource() datasource.DataSource {
	return new(sshKeyDataSource)
}

func (d *sshKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (d *sshKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage SSH Keys",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "SSH Key ID",
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The SSH Key name",
				Required:            true,
			},
			"public_key": schema.StringAttribute{
				MarkdownDescription: "The SSH public key",
				Computed:            true,
			},
		},
	}
}

func (d *sshKeyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*cloudriftapi.HttpClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected DatSource Configure Type",
			fmt.Sprintf("Expected *cloudriftapi.HttpClient, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}

	d.client = client
}

func (d *sshKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model sshKeyDataModel
	diags := req.Config.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := d.client.ListSSHKeys()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading CloudRift SSH Keys",
			"Could not list CloudRift SSH Keys: "+err.Error(),
		)
		return
	}

	for _, k := range keys {
		if k.Name == model.Name.ValueString() {
			model.ID = types.StringValue(k.Id)
			model.PublicKey = types.StringValue(k.PublicKey)
			model.Name = types.StringValue(k.Name)

			diags := resp.State.Set(ctx, &model)
			resp.Diagnostics.Append(diags...)
			return
		}
	}

	resp.Diagnostics.AddError(
		"Error reading CloudRift SSH Key",
		"Could not find CloudRift SSH Key "+model.Name.ValueString(),
	)
}
