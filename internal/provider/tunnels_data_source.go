package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

var (
	_ datasource.DataSource              = &tunnelsDataSource{}
	_ datasource.DataSourceWithConfigure = &tunnelsDataSource{}
)

func NewTunnelsDataSource() datasource.DataSource {
	return &tunnelsDataSource{}
}

type tunnelsDataSource struct {
	client *nubulus.Client
}

type tunnelsDataSourceModel struct {
	// ExternalID narrows the listing to the one tunnel carrying it. Optional:
	// unset lists everything.
	ExternalID types.String `tfsdk:"external_id"`

	Tunnels []tunnelSummaryModel `tfsdk:"tunnels"`
}

// tunnelSummaryModel is a tunnel as a listing shows it.
//
// The credentials are absent and always will be: they are issued once, when the
// tunnel is created. A data source can therefore never produce a usable tunnel,
// only tell you about one — which is worth saying because reaching for this to
// recover a lost token is the obvious thing to try.
type tunnelSummaryModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	ExternalID      types.String `tfsdk:"external_id"`
	AccountID       types.String `tfsdk:"account_id"`
	TunnelSubdomain types.String `tfsdk:"tunnel_subdomain"`
	WireGuardIP     types.String `tfsdk:"wireguard_ip"`
	Status          types.String `tfsdk:"status"`
	OnlineStatus    types.String `tfsdk:"online_status"`
	RouteCount      types.Int64  `tfsdk:"route_count"`
}

func (d *tunnelsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tunnels"
}

func (d *tunnelsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the tunnels of the account the token belongs to.\n\n" +
			"It never carries `tunnel_token` or the WireGuard private key: those are issued once, " +
			"when a tunnel is created, and no read can return them. This tells you what exists, " +
			"not how to run it.",

		Attributes: map[string]schema.Attribute{
			"external_id": schema.StringAttribute{
				MarkdownDescription: "Return only the tunnel carrying this identifier. A tunnel " +
					"that does not exist is an empty list, not an error.",
				Optional: true,
			},
			"tunnels": schema.ListNestedAttribute{
				MarkdownDescription: "The tunnels, in the order the API returns them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Tunnel identifier.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "The label given at creation, if any.",
							Computed:            true,
						},
						"external_id": schema.StringAttribute{
							MarkdownDescription: "The creator's own identifier, if any.",
							Computed:            true,
						},
						"account_id": schema.StringAttribute{
							MarkdownDescription: "Account the tunnel belongs to.",
							Computed:            true,
						},
						"tunnel_subdomain": schema.StringAttribute{
							MarkdownDescription: "The name the platform serves the tunnel on, and " +
								"what a hostname of yours is pointed at with a CNAME.",
							Computed: true,
						},
						"wireguard_ip": schema.StringAttribute{
							MarkdownDescription: "Address assigned to the client end.",
							Computed:            true,
						},
						"status": schema.StringAttribute{
							MarkdownDescription: "Lifecycle of the tunnel. Routes can only be " +
								"written into an `active` one.",
							Computed: true,
						},
						"online_status": schema.StringAttribute{
							MarkdownDescription: "Whether the client end is connected right now: " +
								"`online`, `degraded`, `offline` or `unknown`. It changes on its " +
								"own — a plan that depends on it is a plan that is never stable.",
							Computed: true,
						},
						"route_count": schema.Int64Attribute{
							MarkdownDescription: "How many routes hang off the tunnel.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *tunnelsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (d *tunnelsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config tunnelsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var found []nubulus.TunnelSummary

	if externalID := config.ExternalID.ValueString(); externalID != "" {
		// The filtered lookup answers at most one tunnel, and answers nothing
		// rather than a 404 when there is none — so an empty list here is a
		// result, not a failure to report.
		tunnel, err := d.client.Tunnel.FindTunnelByExternalID(ctx, externalID)
		if err != nil {
			addAPIError(&resp.Diagnostics, "look up the tunnel with external_id "+externalID, err)
			return
		}
		if tunnel != nil {
			found = []nubulus.TunnelSummary{*tunnel}
		}
	} else {
		tunnels, err := d.client.Tunnel.ListTunnels(ctx)
		if err != nil {
			addAPIError(&resp.Diagnostics, "list the tunnels", err)
			return
		}
		found = tunnels
	}

	config.Tunnels = make([]tunnelSummaryModel, 0, len(found))
	for _, t := range found {
		config.Tunnels = append(config.Tunnels, tunnelSummaryModel{
			ID:              types.StringValue(t.ID),
			Name:            stringValue(t.Name),
			ExternalID:      stringValue(t.ExternalID),
			AccountID:       types.StringValue(t.AccountID),
			TunnelSubdomain: types.StringValue(t.TunnelSubdomain),
			WireGuardIP:     types.StringValue(t.WireGuardIP),
			Status:          types.StringValue(t.Status),
			OnlineStatus:    types.StringValue(t.OnlineStatus),
			RouteCount:      types.Int64Value(int64(t.RouteCount)),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
