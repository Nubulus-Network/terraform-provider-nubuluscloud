package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

var (
	_ datasource.DataSource              = &dnsZonesDataSource{}
	_ datasource.DataSourceWithConfigure = &dnsZonesDataSource{}
)

func NewDNSZonesDataSource() datasource.DataSource {
	return &dnsZonesDataSource{}
}

type dnsZonesDataSource struct {
	client *nubulus.Client
}

type dnsZonesDataSourceModel struct {
	Zones []dnsZoneSummaryModel `tfsdk:"zones"`
}

// dnsZoneSummaryModel is the registration alone.
//
// The listing does not carry the serial or the nameservers, and that is the
// API's shape rather than an omission here: those come from reading the zone
// off the primary, and doing it for every zone in an account would turn a
// listing into one DNS transfer per row. Use the nubuluscloud_dns_zone data
// source for a zone you actually need the content of.
type dnsZoneSummaryModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Source     types.String `tfsdk:"source"`
	Status     types.String `tfsdk:"status"`
	AccountID  types.String `tfsdk:"account_id"`
	VerifiedAt types.String `tfsdk:"verified_at"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func (d *dnsZonesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zones"
}

func (d *dnsZonesDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every DNS zone of the account the token belongs to.",

		Attributes: map[string]schema.Attribute{
			"zones": schema.ListNestedAttribute{
				MarkdownDescription: "The zones, in the order the API returns them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.StringAttribute{Computed: true},
						"name":        schema.StringAttribute{Computed: true},
						"source":      schema.StringAttribute{Computed: true},
						"status":      schema.StringAttribute{Computed: true},
						"account_id":  schema.StringAttribute{Computed: true},
						"verified_at": schema.StringAttribute{Computed: true},
						"created_at":  schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *dnsZonesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (d *dnsZonesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	zones, err := d.client.DNS.ListZones(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "list the zones of the account", err)
		return
	}

	state := dnsZonesDataSourceModel{Zones: make([]dnsZoneSummaryModel, 0, len(zones))}
	for _, zone := range zones {
		state.Zones = append(state.Zones, dnsZoneSummaryModel{
			ID:         types.StringValue(zone.ID),
			Name:       types.StringValue(zone.Name),
			Source:     types.StringValue(zone.Source),
			Status:     types.StringValue(zone.Status),
			AccountID:  types.StringValue(zone.AccountID),
			VerifiedAt: timeValue(zone.VerifiedAt),
			CreatedAt:  types.StringValue(zone.CreatedAt.Format(rfc3339)),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
