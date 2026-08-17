package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

var (
	_ resource.Resource                = &tunnelResource{}
	_ resource.ResourceWithImportState = &tunnelResource{}
	_ resource.ResourceWithConfigure   = &tunnelResource{}
)

func NewTunnelResource() resource.Resource {
	return &tunnelResource{}
}

type tunnelResource struct {
	client *nubulus.Client
}

type tunnelResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	ExternalID types.String `tfsdk:"external_id"`

	AdoptExisting types.Bool `tfsdk:"adopt_existing"`

	AccountID       types.String `tfsdk:"account_id"`
	TunnelSubdomain types.String `tfsdk:"tunnel_subdomain"`
	CNAMETarget     types.String `tfsdk:"cname_target"`
	Status          types.String `tfsdk:"status"`
	OnlineStatus    types.String `tfsdk:"online_status"`

	TunnelToken types.String `tfsdk:"tunnel_token"`

	WireGuardIP         types.String `tfsdk:"wireguard_ip"`
	WireGuardPublicKey  types.String `tfsdk:"wireguard_public_key"`
	WireGuardPrivateKey types.String `tfsdk:"wireguard_private_key"`
	WireGuardAddress    types.String `tfsdk:"wireguard_address"`
	WireGuardDNS        types.String `tfsdk:"wireguard_dns"`
	PeerPublicKey       types.String `tfsdk:"peer_public_key"`
	PeerEndpoint        types.String `tfsdk:"peer_endpoint"`
	PeerAllowedIPs      types.String `tfsdk:"peer_allowed_ips"`
}

func (r *tunnelResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tunnel"
}

func (r *tunnelResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A tunnel: an outbound WireGuard connection from a machine of yours to " +
			"the platform, which then serves the hostnames you route through it with " +
			"`nubuluscloud_tunnel_route`.\n\n" +
			"Nothing about the tunnel itself is configurable: the address, the key pair and the " +
			"credential are all issued by the platform. What you choose is only how to recognise it.\n\n" +
			"## The credential is issued once\n\n" +
			"`tunnel_token` and `wireguard_private_key` come back when the tunnel is created and " +
			"are never readable again. They are kept in state and cannot be refreshed from the API; " +
			"a tunnel brought in with `terraform import` has neither, and there is no way to " +
			"recover them. You can only issue new ones, which breaks whatever is using the old.\n\n" +
			"## Set `external_id` if you value your apply being repeatable\n\n" +
			"Without it, an apply interrupted between the create and the state being written leaves " +
			"a tunnel behind that nothing can find again: it holds an address from the pool and a " +
			"credential nobody ever saw, and the next apply makes another one. With it, the next " +
			"apply recognises the tunnel: see `adopt_existing` for what happens then.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Tunnel identifier, assigned by the platform.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "A label for the tunnel. Cosmetic and not unique: nothing keys " +
					"on it.\n\n" +
					"**Changing it replaces the tunnel**, which issues a new credential and takes " +
					"the old one out of service. That is not the intent of renaming something, and " +
					"it is a limitation of the API rather than a choice: there is no operation that " +
					"edits a tunnel in place.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"external_id": schema.StringAttribute{
				MarkdownDescription: "Your own identifier for this tunnel, unique within your " +
					"account. At most 128 characters, no spaces.\n\n" +
					"It is what makes creating a tunnel repeatable: an apply that dies after the " +
					"tunnel exists but before the state is written can be run again, and the " +
					"platform will recognise the tunnel instead of making a second one.\n\n" +
					"Changing it replaces the tunnel.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},

			"adopt_existing": schema.BoolAttribute{
				MarkdownDescription: "What to do when `external_id` already belongs to a tunnel.\n\n" +
					"`false`, the default, **fails** and tells you the two ways out. This is the safe " +
					"answer because the provider cannot tell your own interrupted apply from a " +
					"tunnel that is up and carrying traffic under the same identifier.\n\n" +
					"`true` takes the existing tunnel over and **issues it a new credential**, " +
					"because an adopted tunnel comes back without one and would otherwise be " +
					"unusable. Anything still running on the old credential stops working within " +
					"seconds. Turn it on when the identifier is genuinely yours and unattended " +
					"recovery is worth more than that risk (a pipeline that must converge on its " +
					"own), and leave it off otherwise.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},

			"account_id": schema.StringAttribute{
				MarkdownDescription: "Account the tunnel belongs to, resolved from the token.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"tunnel_subdomain": schema.StringAttribute{
				MarkdownDescription: "The name the platform serves this tunnel on.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"cname_target": schema.StringAttribute{
				MarkdownDescription: "What to point a hostname of yours at with a CNAME, so that " +
					"requests for it arrive through this tunnel.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Lifecycle of the tunnel. Routes can only be written into an " +
					"`active` one.",
				Computed: true,
			},
			"online_status": schema.StringAttribute{
				MarkdownDescription: "Whether the client end is currently connected: `online`, " +
					"`degraded`, `offline` or `unknown`. It changes on its own, with nothing " +
					"applied, so do not build a plan around it.",
				Computed: true,
			},

			"tunnel_token": schema.StringAttribute{
				MarkdownDescription: "The credential the tunnel client authenticates with. Issued " +
					"once and never readable again.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"wireguard_ip": schema.StringAttribute{
				MarkdownDescription: "Address assigned to the client end of the tunnel.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"wireguard_public_key": schema.StringAttribute{
				MarkdownDescription: "Public half of the client key pair.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"wireguard_private_key": schema.StringAttribute{
				MarkdownDescription: "Private half of the client key pair. Issued once and never " +
					"readable again.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"wireguard_address": schema.StringAttribute{
				MarkdownDescription: "`Address` for the client interface, the assigned IP with its mask.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"wireguard_dns": schema.StringAttribute{
				MarkdownDescription: "`DNS` for the client interface.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"peer_public_key": schema.StringAttribute{
				MarkdownDescription: "Public key of the platform end.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"peer_endpoint": schema.StringAttribute{
				MarkdownDescription: "Host and port the client connects out to.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"peer_allowed_ips": schema.StringAttribute{
				MarkdownDescription: "`AllowedIPs` for the peer.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *tunnelResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (r *tunnelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan tunnelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	externalID := plan.ExternalID.ValueString()
	created, err := r.client.Tunnel.CreateTunnel(ctx, nubulus.CreateTunnelInput{
		Name:       plan.Name.ValueString(),
		ExternalID: externalID,
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "create the tunnel", err)
		return
	}

	if created.Adopted {
		// The tunnel already existed under this external_id. Whether that is a
		// retry of an apply that died, or a live tunnel somebody else is
		// running, is not knowable from here: the two are the same answer.
		if !plan.AdoptExisting.ValueBool() {
			resp.Diagnostics.AddError(
				"A tunnel with this external_id already exists",
				"Tunnel "+created.TunnelID+" already carries the external_id \""+externalID+
					"\", so nothing was created.\n\n"+
					"This provider refuses to take it over by default, because it cannot tell an "+
					"apply of yours that was interrupted from a tunnel that is up and serving "+
					"traffic right now.\n\nEither:\n\n"+
					"  * bring it under management as it is, keeping it running:\n"+
					"      terraform import <address> "+created.TunnelID+"\n"+
					"    the credential is NOT recoverable this way, since it is only ever issued once, "+
					"so do this when whatever runs the tunnel already has it; or\n\n"+
					"  * set `adopt_existing = true` to take it over and issue a new credential, "+
					"which stops anything still using the old one; or\n\n"+
					"  * pick a different `external_id`, which creates a separate tunnel.",
			)
			return
		}

		// Asked for explicitly. An adopted tunnel arrives with no credential,
		// because the API will not hand one out twice, so it has to be issued, and
		// that is precisely the part that breaks whatever was using the old.
		tflog.Warn(ctx, "adopting an existing tunnel and rotating its credential", map[string]any{
			"tunnel_id":   created.TunnelID,
			"external_id": externalID,
		})

		rotated, err := r.client.Tunnel.RotateToken(ctx, created.TunnelID)
		if err != nil {
			addAPIError(&resp.Diagnostics,
				"issue a new credential for the adopted tunnel "+created.TunnelID, err)
			return
		}
		created.TunnelToken = rotated.TunnelToken
	}

	applyTunnelCreate(&plan, created)

	// The create answer says nothing about the lifecycle, and the WireGuard
	// public key is only on a read. One read here means the state after an
	// apply is complete rather than half unknown until the next refresh.
	if read, err := r.client.Tunnel.GetTunnel(ctx, created.TunnelID); err != nil {
		tflogWarn(ctx, "the tunnel was created but could not be read back", err)
		// Not fatal, and it must not be: the tunnel exists, and failing here
		// would drop it from state and orphan it. Leaving the two attributes
		// null is recoverable; losing the credential is not.
		plan.Status = types.StringNull()
		plan.OnlineStatus = types.StringNull()
		plan.AccountID = types.StringNull()
		plan.WireGuardPublicKey = types.StringNull()
	} else {
		applyTunnelRead(&plan, read.Tunnel)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tunnelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state tunnelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	read, err := r.client.Tunnel.GetTunnel(ctx, id)
	if err != nil {
		if nubulus.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "read the tunnel "+id, err)
		return
	}

	// THE SECRETS ARE NOT TOUCHED HERE. A read does not carry them, because the
	// platform issues them once, so refreshing them from the API would write
	// empty strings over the only copy that exists. They stay exactly as state
	// already has them, which for an imported tunnel means null.
	applyTunnelRead(&state, read.Tunnel)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update cannot happen: every settable attribute replaces the resource, because
// the API has no operation that edits a tunnel. It fails loudly rather than
// quietly doing nothing, which would leave state disagreeing with the platform.
func (r *tunnelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"A tunnel cannot be updated in place",
		"Nothing about a tunnel is editable through the API, so every settable attribute "+
			"replaces it. Reaching this is a bug in the provider.",
	)
}

func (r *tunnelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state tunnelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	if err := r.client.Tunnel.DeleteTunnel(ctx, id); err != nil {
		if nubulus.IsNotFound(err) {
			return
		}
		addAPIError(&resp.Diagnostics, "delete the tunnel "+id, err)
	}
}

// ImportState takes the tunnel id.
//
// The import is INCOMPLETE by construction and cannot be otherwise: the
// credential and the private key are issued once and are not readable, so an
// imported tunnel carries neither. Everything else is filled in by Read.
func (r *tunnelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// applyTunnelCreate copies the answer to a create, the only place the
// credentials ever appear, over the model.
func applyTunnelCreate(model *tunnelResourceModel, created *nubulus.CreateTunnelResult) {
	model.ID = types.StringValue(created.TunnelID)
	model.TunnelSubdomain = types.StringValue(created.TunnelSubdomain)
	model.CNAMETarget = types.StringValue(created.CNAMETarget)
	model.WireGuardIP = types.StringValue(created.WireGuardIP)

	model.TunnelToken = stringValue(created.TunnelToken)
	model.WireGuardPrivateKey = stringValue(created.WireGuard.Interface.PrivateKey)
	model.WireGuardAddress = stringValue(created.WireGuard.Interface.Address)
	model.WireGuardDNS = stringValue(created.WireGuard.Interface.DNS)
	model.PeerPublicKey = stringValue(created.WireGuard.Peer.PublicKey)
	model.PeerEndpoint = stringValue(created.WireGuard.Peer.Endpoint)
	model.PeerAllowedIPs = stringValue(created.WireGuard.Peer.AllowedIPs)
}

// applyTunnelRead copies what a read can see. It never writes the credentials:
// see Read.
func applyTunnelRead(model *tunnelResourceModel, tunnel *nubulus.Tunnel) {
	if tunnel == nil {
		return
	}

	model.ID = types.StringValue(tunnel.ID)
	model.AccountID = types.StringValue(tunnel.AccountID)
	model.TunnelSubdomain = types.StringValue(tunnel.TunnelSubdomain)
	model.WireGuardIP = types.StringValue(tunnel.WireGuardIP)
	model.WireGuardPublicKey = stringValue(tunnel.WireGuardPublicKey)
	model.Status = types.StringValue(tunnel.Status)
	model.OnlineStatus = types.StringValue(tunnel.OnlineStatus)

	// `name` and `external_id` are configuration, and the API echoes them back
	// exactly as they were sent, so there is nothing to reconcile, except when
	// the tunnel was imported, where state has null and the platform has the
	// value. Writing them only when they are absent adopts the truth without
	// ever overwriting what the configuration says.
	if model.Name.IsNull() && tunnel.Name != "" {
		model.Name = types.StringValue(tunnel.Name)
	}
	if model.ExternalID.IsNull() && tunnel.ExternalID != "" {
		model.ExternalID = types.StringValue(tunnel.ExternalID)
	}

	// The create-only fields are left alone. On an import they are null and
	// stay null, which is the honest report: they cannot be recovered.
	model.CNAMETarget = keepKnownOr(model.CNAMETarget, tunnel.TunnelSubdomain)
}

// keepKnownOr preserves what state already has and falls back to a value the
// API can still derive. It exists for cname_target, which the create reports
// and a read does not, but which is always the tunnel's own subdomain, so an
// imported tunnel gets a correct value instead of a null one.
func keepKnownOr(prior types.String, fallback string) types.String {
	if !prior.IsNull() && !prior.IsUnknown() && prior.ValueString() != "" {
		return prior
	}
	return stringValue(fallback)
}
