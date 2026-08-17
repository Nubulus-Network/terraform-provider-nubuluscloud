package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

var (
	_ resource.Resource                = &dnsZoneResource{}
	_ resource.ResourceWithImportState = &dnsZoneResource{}
	_ resource.ResourceWithConfigure   = &dnsZoneResource{}
)

func NewDNSZoneResource() resource.Resource {
	return &dnsZoneResource{}
}

type dnsZoneResource struct {
	client *nubulus.Client
}

type dnsZoneResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	AccountID types.String `tfsdk:"account_id"`
	Source    types.String `tfsdk:"source"`
	Status    types.String `tfsdk:"status"`

	Nameservers types.List `tfsdk:"nameservers"`

	VerificationRequired types.Bool   `tfsdk:"verification_required"`
	VerificationTXTHost  types.String `tfsdk:"verification_txt_host"`
	VerificationTXTValue types.String `tfsdk:"verification_txt_value"`
	ReservedUntil        types.String `tfsdk:"reserved_until"`
	VerifiedAt           types.String `tfsdk:"verified_at"`
	Serial               types.Int64  `tfsdk:"serial"`
	PrimaryError         types.String `tfsdk:"primary_error"`
	CreatedAt            types.String `tfsdk:"created_at"`
}

func (r *dnsZoneResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone"
}

func (r *dnsZoneResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A DNS zone served by the Nubulus name servers.\n\n" +
			"Creating this resource does not necessarily create anything on the name servers, and " +
			"which of the two happens is not a choice:\n\n" +
			"* a domain already assigned to the account is created and `active` immediately;\n" +
			"* any other name is **reserved**, with `status = \"pending_verification\"`, and nothing " +
			"exists on the name servers until control of the name has been proven. Publish the TXT " +
			"record in `verification_txt_host`/`verification_txt_value` wherever the domain resolves " +
			"today, then use `nubuluscloud_dns_zone_verification`.\n\n" +
			"The order is a safety property, not a workflow preference: a zone created before " +
			"control is proven answers authoritatively for a name that is not yours.\n\n" +
			"Creating and destroying a zone needs a token with the `owner` or `admin` role. " +
			"`member` can edit records but not zones.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Zone identifier, `zone_` followed by a ULID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Zone name, without a trailing dot. Changing it replaces the zone, " +
					"which takes the old one off the internet.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"account_id": schema.StringAttribute{
				MarkdownDescription: "Account the zone belongs to, resolved from the token.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"source": schema.StringAttribute{
				MarkdownDescription: "`neodigit` for a domain already assigned to the account, " +
					"`external` for a name registered elsewhere.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`pending_verification`, `active` or `suspended`. Records can only " +
					"be written into an `active` zone.",
				Computed: true,
			},
			"nameservers": schema.ListAttribute{
				MarkdownDescription: "The name servers to delegate the domain to. Present in both " +
					"states: this is also what has to be published at the registrar for the zone to be " +
					"used at all.",
				ElementType: types.StringType,
				Computed:    true,
			},
			"verification_required": schema.BoolAttribute{
				MarkdownDescription: "Whether control of the name still has to be proven.",
				Computed:            true,
			},
			"verification_txt_host": schema.StringAttribute{
				MarkdownDescription: "Name of the challenge TXT record, `_nubulus-challenge.<zone>`. " +
					"Null once there is nothing left to prove.",
				Computed: true,
			},
			"verification_txt_value": schema.StringAttribute{
				MarkdownDescription: "Value the challenge TXT record must carry.",
				Computed:            true,
			},
			"reserved_until": schema.StringAttribute{
				MarkdownDescription: "When an unproven claim lapses and the name goes back to being " +
					"free, in RFC 3339.",
				Computed: true,
			},
			"verified_at": schema.StringAttribute{
				MarkdownDescription: "When control of the name was proven, in RFC 3339.",
				Computed:            true,
			},
			"serial": schema.Int64Attribute{
				MarkdownDescription: "SOA serial read from the primary. Null while the zone is pending, " +
					"and also when the primary could not be read: see `primary_error`.",
				Computed: true,
			},
			"primary_error": schema.StringAttribute{
				MarkdownDescription: "Why the primary could not be read: `XFR_REFUSED`, `XFR_TSIG`, " +
					"`XFR_TIMEOUT`, `XFR_DISABLED` or `XFR_FAILED`. Reading the zone does not fail when " +
					"this happens, because the registration is real information and does not stop being " +
					"true when a name server is unreachable, so the reason is reported here instead.",
				Computed: true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When the zone was claimed, in RFC 3339.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *dnsZoneResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (r *dnsZoneResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsZoneResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	if _, ok := nubulus.NormalizeZoneName(name); !ok {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Invalid zone name",
			"`"+name+"` is not a valid zone name: it needs at least two labels and only "+
				"letters, digits and hyphens.")
		return
	}

	detail, err := r.client.DNS.CreateZone(ctx, name)
	if err != nil {
		addAPIError(&resp.Diagnostics, "create the zone "+name, err)
		return
	}

	// A zone that came back pending is the normal path for an external name and
	// is NOT reported as a failure here. It is said out loud, because the next
	// thing that happens otherwise is somebody wondering why their records will
	// not write.
	if detail.Zone != nil && detail.Zone.Status == "pending_verification" {
		tflog.Info(ctx, "zone reserved, pending verification", map[string]any{"zone": name})
	}

	applyZoneDetail(&plan, detail)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsZoneResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsZoneResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	detail, err := r.client.DNS.GetZone(ctx, name)
	if err != nil {
		if nubulus.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "read the zone "+name, err)
		return
	}

	applyZoneDetail(&state, detail)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update cannot happen: `name` is the only settable attribute and it replaces
// the resource. The method exists because the interface requires it, and it
// fails loudly rather than silently doing nothing, which would leave state
// disagreeing with the platform.
func (r *dnsZoneResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"A zone cannot be updated in place",
		"Nothing about a zone is editable: its name identifies it and everything else is derived. "+
			"Reaching this is a bug in the provider.",
	)
}

func (r *dnsZoneResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsZoneResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	if err := r.client.DNS.DeleteZone(ctx, name); err != nil {
		if nubulus.IsNotFound(err) {
			return
		}
		addAPIError(&resp.Diagnostics, "delete the zone "+name, err)
	}
}

// ImportState takes the zone name, not the id.
//
// The id is a ULID nobody has written down, while the name is what the person
// importing is looking at. Read fills in everything else, id included.
func (r *dnsZoneResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// applyZoneDetail copies an API answer over the model. It is a package
// function rather than a method because the data source fills in the same
// model from the same answer, and two copies of this would drift.
func applyZoneDetail(model *dnsZoneResourceModel, detail *nubulus.ZoneDetail) {
	zone := detail.Zone
	if zone != nil {
		model.ID = types.StringValue(zone.ID)
		// `name` is NOT written back, and that is not an omission. The API
		// normalizes it (lowercased, trailing dot removed), so a configuration
		// that said "Ejemplo.com." would get a different string in state than
		// it planned, which Terraform reports as "Provider produced
		// inconsistent result after apply", and which on the next plan would
		// look like a zone that has to be replaced.
		model.AccountID = types.StringValue(zone.AccountID)
		model.Source = types.StringValue(zone.Source)
		model.Status = types.StringValue(zone.Status)
		model.ReservedUntil = timeValue(zone.ReservedUntil)
		model.VerifiedAt = timeValue(zone.VerifiedAt)
		model.CreatedAt = types.StringValue(zone.CreatedAt.Format(rfc3339))
	}

	model.PrimaryError = stringValue(detail.PrimaryError)

	if detail.Serial != nil {
		model.Serial = types.Int64Value(int64(*detail.Serial))
	} else {
		model.Serial = types.Int64Null()
	}

	// The two sources of nameservers are exclusive by construction: a pending
	// zone has nothing on the primary to transfer, so its delegation comes with
	// the verification instructions instead. Preferring the instructions when
	// they are there keeps the attribute populated in both states, which is what
	// makes it usable as the thing you hand to the registrar.
	nameservers := detail.Nameservers
	if detail.Verification != nil {
		model.VerificationRequired = types.BoolValue(detail.Verification.Required)
		model.VerificationTXTHost = stringValue(detail.Verification.TXTRecordHost)
		model.VerificationTXTValue = stringValue(detail.Verification.TXTRecordValue)
		if len(detail.Verification.Nameservers) > 0 {
			nameservers = detail.Verification.Nameservers
		}
	} else {
		model.VerificationRequired = types.BoolValue(false)
		// THE CHALLENGE IS NOT BLANKED ONCE IT HAS BEEN KNOWN, and this is not
		// cosmetic: it is what keeps the documented flow working after it has
		// worked.
		//
		// The API stops reporting the challenge the moment there is nothing
		// left to prove, so a verified zone reports none. But the pattern this
		// provider tells people to write publishes the challenge with
		// `values = ["\"${...verification_txt_value}\""]`, and Terraform
		// evaluates that expression on every later plan, including the destroy.
		// Letting the attribute fall back to null turns every subsequent
		// command into "Invalid template interpolation value: the expression
		// result is null", with no way out except editing the configuration
		// that just worked.
		//
		// The token itself does not change and is still held server-side, so
		// keeping the known value is also the truthful thing to report.
		model.VerificationTXTHost = keepKnown(model.VerificationTXTHost)
		model.VerificationTXTValue = keepKnown(model.VerificationTXTValue)
	}

	model.Nameservers = stringList(nameservers)
}

// keepKnown preserves a value already in state when the API stops reporting it.
// An unknown (a fresh plan) or an absent one stays null.
func keepKnown(prior types.String) types.String {
	if prior.IsUnknown() || prior.IsNull() {
		return types.StringNull()
	}
	return prior
}
