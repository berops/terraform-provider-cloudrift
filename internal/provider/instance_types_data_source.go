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
	_ datasource.DataSource              = &instanceTypesSource{}
	_ datasource.DataSourceWithConfigure = &instanceTypesSource{}
)

type instanceTypeVariantDatacenterModel struct {
	Name  types.String `tfsdk:"name"`
	Count types.Int64  `tfsdk:"count"`
}

type instanceTypeVariantModel struct {
	Name        types.String                         `tfsdk:"name"`
	CpuCount    types.Int64                          `tfsdk:"cpu_count"`
	GpuCount    types.Int64                          `tfsdk:"gpu_count"`
	Disk        types.Int64                          `tfsdk:"disk"`
	DRAM        types.Int64                          `tfsdk:"dram"`
	CostPerHour types.Float64                        `tfsdk:"cost_per_hour"`
	Datacenters []instanceTypeVariantDatacenterModel `tfsdk:"datacenters"`
}

type instanceTypeModel struct {
	Name         types.String               `tfsdk:"name"`
	BrandShort   types.String               `tfsdk:"brand_short"`
	Manufacturer types.String               `tfsdk:"manufacturer"`
	Variants     []instanceTypeVariantModel `tfsdk:"variants"`
}

type listInstanceTypesModel struct {
	InstanceTypes []instanceTypeModel `tfsdk:"instance_types"`
}

type instanceTypesSource struct {
	client *cloudriftapi.HttpClient
}

func NewInstanceTypesDataSource() datasource.DataSource {
	return new(instanceTypesSource)
}

func (d *instanceTypesSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_types"
}

func (d *instanceTypesSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read Instance Types",
		Attributes: map[string]schema.Attribute{
			"instance_types": schema.ListNestedAttribute{
				MarkdownDescription: "Known Instance Types",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Name of the Instance Type",
							Computed:            true,
						},
						"brand_short": schema.StringAttribute{
							MarkdownDescription: "Short name of the Brand",
							Computed:            true,
						},
						"manufacturer": schema.StringAttribute{
							MarkdownDescription: "Name of the Manufacturer",
							Computed:            true,
						},
						"variants": schema.ListNestedAttribute{
							MarkdownDescription: "Variants of the Instance Type	",
							Computed:            true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"name": schema.StringAttribute{
										MarkdownDescription: "Name of the Instance Type Variant",
										Computed:            true,
									},
									"cpu_count": schema.Int64Attribute{
										MarkdownDescription: "Number of CPUs",
										Computed:            true,
									},
									"gpu_count": schema.Int64Attribute{
										MarkdownDescription: "Number of GPUs",
										Computed:            true,
									},
									"disk": schema.Int64Attribute{
										MarkdownDescription: "Disk size",
										Computed:            true,
									},
									"dram": schema.Int64Attribute{
										MarkdownDescription: "DRAM size",
										Computed:            true,
									},
									"cost_per_hour": schema.Float64Attribute{
										MarkdownDescription: "Cost per Hour",
										Computed:            true,
									},
									"datacenters": schema.ListNestedAttribute{
										MarkdownDescription: "Datacenters for the Instance Variant Type",
										Computed:            true,
										NestedObject: schema.NestedAttributeObject{
											Attributes: map[string]schema.Attribute{
												"name": schema.StringAttribute{
													MarkdownDescription: "Name of the datacenter",
													Computed:            true,
												},
												"count": schema.Int64Attribute{
													MarkdownDescription: "Number of instances of this variant in the datacenter. Does not equal to the number of currently available instances in the datacenter of this variant",
													Computed:            true,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *instanceTypesSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *instanceTypesSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model listInstanceTypesModel
	diags := req.Config.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	t, err := d.client.ListInstanceTypes()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading CloudRift Instance Types",
			"Could not list CloudRift Instance Types: "+err.Error(),
		)
		return
	}

	var instanceTypes []instanceTypeModel
	for _, t := range t.Data.InstanceTypes {
		i := instanceTypeModel{
			Name: types.StringValue(t.Name),
		}
		if t.BrandShort != nil {
			i.BrandShort = types.StringValue(*t.BrandShort)
		}
		if t.Manufacturer != nil {
			i.Manufacturer = types.StringValue(*t.Manufacturer)
		}

		for _, v := range t.Variants {
			n := instanceTypeVariantModel{
				Name:        types.StringValue(v.Name),
				CpuCount:    types.Int64Value(int64(v.CpuCount)),
				Disk:        types.Int64Value(v.Disk),
				DRAM:        types.Int64Value(v.Dram),
				CostPerHour: types.Float64Value(v.CostPerHour),
			}
			if v.GpuCount != nil {
				n.GpuCount = types.Int64Value(int64(*v.GpuCount))
			}
			for k, v := range v.NodesPerDc {
				n.Datacenters = append(n.Datacenters, instanceTypeVariantDatacenterModel{
					Name:  types.StringValue(k),
					Count: types.Int64Value(int64(v)),
				})
			}
			i.Variants = append(i.Variants, n)
		}
		instanceTypes = append(instanceTypes, i)
	}

	model.InstanceTypes = instanceTypes
	diags = resp.State.Set(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
