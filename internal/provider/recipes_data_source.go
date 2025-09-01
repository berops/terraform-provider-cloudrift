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
	_ datasource.DataSource              = &recipesDataSource{}
	_ datasource.DataSourceWithConfigure = &recipesDataSource{}
)

type recipeDataModel struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

type groupDataModel struct {
	Name        types.String      `tfsdk:"name"`
	Description types.String      `tfsdk:"description"`
	Recipes     []recipeDataModel `tfsdk:"recipes"`
}

type recipesDataModel struct {
	Groups []groupDataModel `tfsdk:"groups"`
}

type recipesDataSource struct {
	client *cloudriftapi.HttpClient
}

func NewRecipesDataSource() datasource.DataSource {
	return new(recipesDataSource)
}

func (d *recipesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_recipes"
}

func (d *recipesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read recipes",
		Attributes: map[string]schema.Attribute{
			"groups": schema.ListNestedAttribute{
				MarkdownDescription: "Grouped recipes",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Name of the group containing related recipes",
							Computed:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "Description of the group",
							Computed:            true,
						},
						"recipes": schema.ListNestedAttribute{
							MarkdownDescription: "Recipes beloning to the group",
							Computed:            true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"name": schema.StringAttribute{
										MarkdownDescription: "Name of the recipe",
										Computed:            true,
									},
									"description": schema.StringAttribute{
										MarkdownDescription: "Description of the recipe",
										Computed:            true,
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

func (d *recipesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *recipesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model recipesDataModel
	diags := req.Config.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	groups, err := d.client.ListRecipes()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading CloudRift recipes",
			"Could not list CloudRift recipes: "+err.Error(),
		)
		return
	}

	var recipeGroups []groupDataModel
	for _, g := range groups.Data.Groups {
		group := groupDataModel{
			Name:        types.StringValue(g.Name),
			Description: types.StringValue(g.Description),
		}

		for _, r := range g.Recipes {
			group.Recipes = append(group.Recipes, recipeDataModel{
				Name:        types.StringValue(r.Name),
				Description: types.StringValue(r.Description),
			})
		}

		recipeGroups = append(recipeGroups, group)
	}

	model.Groups = recipeGroups

	diags = resp.State.Set(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
