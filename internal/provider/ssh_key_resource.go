package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/berops/terraform-provider-cloudrift/pkg/cloudriftapi"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &sshKeyResource{}
	_ resource.ResourceWithConfigure   = &sshKeyResource{}
	_ resource.ResourceWithImportState = &sshKeyResource{}
)

type sshKeyModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	PublicKey types.String `tfsdk:"public_key"`
}

type sshKeyResource struct {
	client *cloudriftapi.HttpClient
}

func NewSSHKeyResource() resource.Resource {
	return &sshKeyResource{}
}

func (r *sshKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (r *sshKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*cloudriftapi.HttpClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *cloudriftapi.HttpClient, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}

	r.client = client
}

func (r *sshKeyResource) Schema(_ context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage SSH Keys",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "SSH Key ID",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(), // immutable
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The SSH Key name",
				Required:            true,
			},
			"public_key": schema.StringAttribute{
				MarkdownDescription: "The SSH public key",
				Required:            true,
			},
		},
	}
}

func (r *sshKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sshKeyModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.Name.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Error creating SSH key",
			"Name is defined but empty",
		)
		return
	}

	if plan.PublicKey.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Error creating SSH key",
			"Public key is defined but empty",
		)
		return
	}

	key, err := r.client.AddSSHKey(plan.Name.ValueString(), plan.PublicKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating SSH key",
			"Could not create SSH Key, unexpected error: "+err.Error(),
		)
		return
	}

	plan.ID = types.StringValue(key.Data.PublicKey.Id)
	plan.Name = types.StringValue(key.Data.PublicKey.Name)
	plan.PublicKey = types.StringValue(key.Data.PublicKey.PublicKey)

	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *sshKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// refresh tf state with latest data.
	var state sshKeyModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := r.client.ListSSHKeys()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading CloudRift SSH Keys",
			"Could not list CloudRift SSH Keys: "+err.Error(),
		)
		return
	}

	var matched *cloudriftapi.SshKey
	for _, k := range keys {
		if k.Id == state.ID.ValueString() {
			matched = &k
			break
		}
	}

	if matched == nil {
		resp.State.RemoveResource(ctx) // remove from state, will force recreate
		return
	}

	state.ID = types.StringValue(matched.Id)
	state.Name = types.StringValue(matched.Name)
	state.PublicKey = types.StringValue(matched.PublicKey)

	// update tf state.
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *sshKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Unsupported Method",
		"Update is not supported for CloudRift SSH Key",
	)
}

func (r *sshKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sshKeyModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteSSHKey(state.ID.ValueString()); err != nil {
		if errors.Is(err, cloudriftapi.ErrNotFound) {
			// resource already deleted from outside the terraform state.
			return
		}
		resp.Diagnostics.AddError(
			"Error Delete SSH Key",
			"Could not delete SSH Key ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}
}

func (r *sshKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
