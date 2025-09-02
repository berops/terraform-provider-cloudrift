package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/berops/terraform-provider-cloudrift/pkg/cloudriftapi"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Polling internval for the creation/destruction of resources.
const InstancePollingInterval = 5 * time.Second

var (
	_ resource.Resource                = &virtualMachineResource{}
	_ resource.ResourceWithConfigure   = &virtualMachineResource{}
	_ resource.ResourceWithImportState = &virtualMachineResource{}
)

type virtualMachineMetadataModel struct {
	StartupCommands types.String `tfsdk:"startup_commands"`
}

type virtualMachineInfoModel struct {
	VmID     types.Int64  `tfsdk:"vmid"`
	Name     types.String `tfsdk:"name"`
	Username types.String `tfsdk:"username"`
}

type virtualMachineModel struct {
	ID     types.String `tfsdk:"id"`
	Status types.String `tfsdk:"status"`

	NodeId     types.String `tfsdk:"node_id"`
	NodeMode   types.String `tfsdk:"node_mode"`
	NodeStatus types.String `tfsdk:"node_status"`

	PublicIP  types.String `tfsdk:"public_ip"`
	PrivateIP types.String `tfsdk:"private_ip"`

	ProviderName types.String `tfsdk:"provider_name"`
	InstanceType types.String `tfsdk:"instance_type"`

	VirtualMachines types.List `tfsdk:"virtual_machines"`

	// Write only attributes.
	Metadata   *virtualMachineMetadataModel `tfsdk:"metadata"`
	Recipe     types.String                 `tfsdk:"recipe"`
	Datacenter types.String                 `tfsdk:"datacenter"`
	SSHKeyID   types.String                 `tfsdk:"ssh_key_id"`
}

type virtualMachineResource struct {
	client *cloudriftapi.HttpClient
}

func NewInstanceResource() resource.Resource {
	return new(virtualMachineResource)
}

func (r *virtualMachineResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_machine"
}

func (r *virtualMachineResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *virtualMachineResource) Schema(_ context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage virtualMachines",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Instance ID",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(), // immutable
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "The status of the virtual machine",
				Computed:            true,
			},
			"node_id": schema.StringAttribute{
				MarkdownDescription: "ID of the node where the Virtual Machine is running on.",
				Computed:            true,
			},
			"node_mode": schema.StringAttribute{
				MarkdownDescription: "Mode of the Node the Virtual Machine is running on.",
				Computed:            true,
			},
			"node_status": schema.StringAttribute{
				MarkdownDescription: "Status fo the Node the Virtual Machine is running on.",
				Computed:            true,
			},
			"public_ip": schema.StringAttribute{
				MarkdownDescription: "The public IPv4 IP address",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(), // immutable
				},
			},
			"private_ip": schema.StringAttribute{
				MarkdownDescription: "The private IP address",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(), // immutable
				},
			},
			"provider_name": schema.StringAttribute{
				MarkdownDescription: "The name of the provider.",
				Computed:            true,
			},
			"instance_type": schema.StringAttribute{
				MarkdownDescription: "The instance type identifier",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"virtual_machines": schema.ListNestedAttribute{
				MarkdownDescription: "Virtual Machines info.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"vmid": schema.Int64Attribute{
							MarkdownDescription: "ID of the VM",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Name assigned to the Virtual Machine by the CloudRift Platform.",
							Computed:            true,
						},
						"username": schema.StringAttribute{
							MarkdownDescription: "Username Generated by the CloudRift API to SSH into the Virtual Machine.",
							Computed:            true,
						},
					},
				},
			},
			"metadata": schema.SingleNestedAttribute{
				MarkdownDescription: "Option to provide metadata. Currently supported is `startup_commands`.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"startup_commands": schema.StringAttribute{
						MarkdownDescription: "A plain text script that will be executed after the first instance boot.",
						Optional:            true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.RequiresReplace(),
						},
					},
				},
			},
			"recipe": schema.StringAttribute{
				MarkdownDescription: "The Base Image used for the Virtual Machine",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"datacenter": schema.StringAttribute{
				MarkdownDescription: "The datacenter identifier",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"ssh_key_id": schema.StringAttribute{
				MarkdownDescription: "The SSH Key ID to be able to connect to the Virtual Machine.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *virtualMachineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan virtualMachineModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := r.client.ListSSHKeys()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading CloudRift SSH Keys",
			"Could not list CloudRift SSH Keys needed for the Virtual Machine: "+err.Error(),
		)
		return
	}

	var matched *cloudriftapi.SshKey
	for _, k := range keys {
		if k.Id == plan.SSHKeyID.ValueString() {
			matched = &k
			break
		}
	}

	if matched == nil {
		resp.Diagnostics.AddError(
			"Error fetching Virtual Machine SSH Key",
			"Could not fetch Virtual Machine SSH Key with ID: "+plan.SSHKeyID.ValueString()+" as it doest not exists",
		)
		return
	}

	startupCommands := ""
	if plan.Metadata != nil {
		startupCommands = plan.Metadata.StartupCommands.ValueString()
	}

	ids, err := r.client.RentPublicInstanceVM(
		plan.Recipe.ValueString(),
		plan.Datacenter.ValueString(),
		plan.InstanceType.ValueString(),
		startupCommands,
		[]string{matched.PublicKey},
	)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating Virtual Machine",
			"Could not create Virtual Machine, unexpected error: "+err.Error(),
		)
		return
	}

	if len(ids.Data.InstanceIds) < 1 {
		// Based on https://github.com/dstackai/dstack/pull/2771/files
		// We should expect 1 instance ID.
		resp.Diagnostics.AddError(
			"Error creating Virtual Machine",
			"Could not create Virtual Machine, no valid IDs were returned from the CloudRift server",
		)
		return
	}

	id := ids.Data.InstanceIds[0]
	plan.ID = types.StringValue(id) // always assing atleast the ID to the state file.

	var last *cloudriftapi.InstanceAndUsageInfo

	// The provisioning timeout is generous here, as usually the VM is provisioned
	// within 2 - 6 mins. The timeout here is in case of failure so that the we eventually
	// exit and don't wait for the VM infinitely.
	provisioningTimeout := time.After(28 * time.Minute)

	// we have successfully rented out the VM. Poll until finished creating, or timeout is reached.
	for {
		select {
		case <-provisioningTimeout:
			// Failed to provisioning VM within the requested timeout.
			//
			// If there was some state previously fetched, use it to store it in the state file.
			if last != nil {
				resp.Diagnostics.Append(populateModelFromInstanceResponse(&plan, last)...)
			}

			// Save any partial state into the state file.
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

			resp.Diagnostics.AddError(
				"Provisioning timeout reached",
				"Provisioning timeout reached before finished waiting on instance creation",
			)

			return
		case <-ctx.Done():
			// Context cancelled.
			//
			// If there was some state previously fetched, use it to store it in the state file.
			if last != nil {
				resp.Diagnostics.Append(populateModelFromInstanceResponse(&plan, last)...)
			}

			// Save any partial state into the state file.
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

			if err := ctx.Err(); err != nil {
				resp.Diagnostics.AddError(
					"Polling Interval Canceled",
					"Polling interval canceled before finished waiting on instance creation: "+err.Error(),
				)
			}

			return
		case <-time.After(InstancePollingInterval):
			current, err := r.client.GetInstance(id)
			if err != nil {
				if !errors.Is(err, cloudriftapi.ErrNotFound) {
					if last != nil {
						resp.Diagnostics.Append(populateModelFromInstanceResponse(&plan, last)...)
					}
					// If there was an API error, store the last know state of the virtual machine
					// in the state file.
					resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
				}
				resp.Diagnostics.AddError(
					"Error creating Virtual Machine",
					"Could not create Virtual Machine, failed to poll status of the rented Virtual Machine ID: "+id+" : "+err.Error(),
				)
				return
			}

			last = current

			// Currently it is only one VM per instance, while the [Status] field
			// tells us that the Instance is spawned successfully, it does not tell us
			// if we are ready to SSH into it. Based on how the Frontend implemented it,
			// it seems to be checking the [VirtualMachines] array for readiness after
			// which it signals that the user can connect to the VM, we try to mimic this
			// here.
			vmReady := false
			if len(last.VirtualMachines) > 0 && last.VirtualMachines[0].Ready {
				vmReady = true
			}

			if last.Status == cloudriftapi.Active && vmReady {
				resp.Diagnostics.Append(populateModelFromInstanceResponse(&plan, last)...)
				resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
				if resp.Diagnostics.HasError() {
					return
				}
				return
			}
		}
	}
}

func (r *virtualMachineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// refresh tf state with latest data.
	var state virtualMachineModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	vm, err := r.client.GetInstance(state.ID.ValueString())
	if err != nil {
		if errors.Is(err, cloudriftapi.ErrNotFound) {
			resp.State.RemoveResource(ctx) // remove from state, will force recreate.
			return
		}
		resp.Diagnostics.AddError(
			"Error reading CloudRift Virtual Machine",
			"Cloud not fetch CloudRift Virtual Machine with ID: "+state.ID.ValueString()+" : "+err.Error(),
		)
		return
	}

	diags = populateModelFromInstanceResponse(&state, vm)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// update tf state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *virtualMachineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Currently Cloudrift does not seem to have an API for updating Rented Virtual Machine Instances
	// As of now, there seems to be only Rent/Terminate endpoint.
	resp.Diagnostics.AddError(
		"Unsupported Method",
		"Update is not supported for CloudRfit Virtual Machine Instance",
	)
}

func (r *virtualMachineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state virtualMachineModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.TerminateInstance(state.ID.ValueString()); err != nil {
		if errors.Is(err, cloudriftapi.ErrNotFound) {
			// resource already deleted from outside the terraform state.
			return
		}
		resp.Diagnostics.AddError(
			"Error Delete Virtual Machine",
			"Could not delete Virtual Machine with ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	for {
		select {
		case <-ctx.Done():
			// Context cancelled.
			//
			// The Instance was successfully marked for deletion
			// so exiting here shouldn't cause any problems.
			if err := ctx.Err(); err != nil {
				resp.Diagnostics.AddError(
					"Polling Interval Canceled",
					"Polling interval canceled before finished waiting on instance destruction: "+err.Error(),
				)
			}
			return
		case <-time.After(InstancePollingInterval):
			// wait until the Instance is no longer returned by the CloudRift API.
			if _, err := r.client.GetInstance(state.ID.ValueString()); err != nil {
				if errors.Is(err, cloudriftapi.ErrNotFound) {
					// successfully destroyed.
					return
				}
				resp.Diagnostics.AddError(
					"Client Error",
					"Polling Instance while marked for deletion: "+err.Error(),
				)
				return
			}
		}
	}
}

func populateModelFromInstanceResponse(m *virtualMachineModel, data *cloudriftapi.InstanceAndUsageInfo) []diag.Diagnostic {
	var diags []diag.Diagnostic

	m.ID = types.StringValue(data.Id)
	m.Status = types.StringValue(string(data.Status))
	m.NodeId = types.StringValue(data.NodeId)
	m.NodeMode = types.StringValue(string(data.NodeMode))
	m.NodeStatus = types.StringValue(string(data.NodeStatus))
	if data.HostAddress != nil {
		m.PublicIP = types.StringValue(*data.HostAddress)
	}
	if data.InternalHostAddress != nil {
		m.PrivateIP = types.StringValue(*data.InternalHostAddress)
	}
	if data.ResourceInfo != nil {
		m.ProviderName = types.StringValue(data.ResourceInfo.ProviderName)
		m.InstanceType = types.StringValue(data.ResourceInfo.InstanceType)
	}

	var vms []attr.Value
	for _, vm := range data.VirtualMachines {
		model := virtualMachineInfoModel{
			VmID: types.Int64Value(int64(vm.Vmid)),
			Name: types.StringValue(vm.Name),
		}
		if login := vm.LoginInfo; login != nil {
			if login, err := login.AsInstanceLoginInfo0(); err == nil {
				model.Username = types.StringValue(login.UsernameAndPassword.Username)
			}
		}

		obj, d := types.ObjectValue(map[string]attr.Type{
			"vmid":     types.Int64Type,
			"name":     types.StringType,
			"username": types.StringType,
		}, map[string]attr.Value{
			"vmid":     model.VmID,
			"name":     model.Name,
			"username": model.Username,
		})

		diags = append(diags, d...)
		vms = append(vms, obj)
	}

	var valueDiags diag.Diagnostics
	m.VirtualMachines, valueDiags = types.ListValue(types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"vmid":     types.Int64Type,
			"name":     types.StringType,
			"username": types.StringType,
		},
	}, vms)

	diags = append(diags, valueDiags...)

	// Since write-only attributes are supported on newer tf versions, have a workaround.
	// Carry over the previous state for the write only attributes, since the API for fetching
	// Instances does not return these.
	// https://discuss.hashicorp.com/t/handling-attribute-required-during-create-but-not-returned-during-read/74613
	//
	// state.Metada = state.Metadata
	// state.Recipe = state.Recipe
	// state.Datacenter = state.Datacenter
	// state.SSHKeyID = state.SSHKeyID

	return diags

}

func (r *virtualMachineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
