package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

var (
	_ datasource.DataSource              = &dnsZoneDataSource{}
	_ datasource.DataSourceWithConfigure = &dnsZoneDataSource{}
)

func NewDNSZoneDataSource() datasource.DataSource {
	return &dnsZoneDataSource{}
}

type dnsZoneDataSource struct {
	client *nubulus.Client
}

// It reuses the resource's model so the two cannot drift apart: an attribute
// added to one and forgotten in the other is the commonest way a provider ends
// up describing the same object two different ways.
type dnsZoneDataSourceModel = dnsZoneResourceModel

func (d *dnsZoneDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone"
}

func (d *dnsZoneDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a DNS zone of the account, whether or not Terraform created it. " +
			"Useful for adding records to a zone that was claimed from the panel.",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Zone name, without a trailing dot.",
				Required:            true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Zone identifier.",
				Computed:            true,
			},
			"account_id": schema.StringAttribute{
				MarkdownDescription: "Account the zone belongs to.",
				Computed:            true,
			},
			"source": schema.StringAttribute{
				MarkdownDescription: "`neodigit` or `external`.",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`pending_verification`, `active` or `suspended`.",
				Computed:            true,
			},
			"nameservers": schema.ListAttribute{
				MarkdownDescription: "The name servers the domain should be delegated to.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"verification_required": schema.BoolAttribute{
				MarkdownDescription: "Whether control of the name still has to be proven.",
				Computed:            true,
			},
			"verification_txt_host": schema.StringAttribute{
				MarkdownDescription: "Name of the challenge TXT record.",
				Computed:            true,
			},
			"verification_txt_value": schema.StringAttribute{
				MarkdownDescription: "Value of the challenge TXT record.",
				Computed:            true,
			},
			"reserved_until": schema.StringAttribute{
				MarkdownDescription: "When an unproven claim lapses, in RFC 3339.",
				Computed:            true,
			},
			"verified_at": schema.StringAttribute{
				MarkdownDescription: "When control was proven, in RFC 3339.",
				Computed:            true,
			},
			"serial": schema.Int64Attribute{
				MarkdownDescription: "SOA serial read from the primary.",
				Computed:            true,
			},
			"primary_error": schema.StringAttribute{
				MarkdownDescription: "Why the primary could not be read, when it could not.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When the zone was claimed, in RFC 3339.",
				Computed:            true,
			},
		},
	}
}

func (d *dnsZoneDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (d *dnsZoneDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config dnsZoneDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := config.Name.ValueString()
	detail, err := d.client.DNS.GetZone(ctx, name)
	if err != nil {
		addAPIError(&resp.Diagnostics, "read the zone "+name, err)
		return
	}

	applyZoneDetail(&config, detail)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
