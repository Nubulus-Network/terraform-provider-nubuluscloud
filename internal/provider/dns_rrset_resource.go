package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

var (
	_ resource.Resource                   = &dnsRRsetResource{}
	_ resource.ResourceWithImportState    = &dnsRRsetResource{}
	_ resource.ResourceWithConfigure      = &dnsRRsetResource{}
	_ resource.ResourceWithValidateConfig = &dnsRRsetResource{}
)

func NewDNSRRsetResource() resource.Resource {
	return &dnsRRsetResource{}
}

type dnsRRsetResource struct {
	client *nubulus.Client
}

type dnsRRsetResourceModel struct {
	ID     types.String `tfsdk:"id"`
	Zone   types.String `tfsdk:"zone"`
	Name   types.String `tfsdk:"name"`
	Type   types.String `tfsdk:"type"`
	FQDN   types.String `tfsdk:"fqdn"`
	TTL    types.Int64  `tfsdk:"ttl"`
	Values types.Set    `tfsdk:"values"`
}

func (r *dnsRRsetResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_rrset"
}

func (r *dnsRRsetResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A DNS record **set**: every record that shares a name and a type.\n\n" +
			"The set is the unit and not the individual record, because that is what the DNS protocol " +
			"operates on. Three A records on `www` are one `nubuluscloud_dns_rrset` with three " +
			"`values`, never three resources — modelling them separately would have each apply race " +
			"against the other two.\n\n" +
			"Writing records needs a token with the `member` role or above, and a zone that is " +
			"`active`.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "`<zone>/<fqdn>/<type>`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone": schema.StringAttribute{
				MarkdownDescription: "Zone the record set lives in.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Record name. Relative to the zone (`www`), fully qualified " +
					"(`www.example.com` or `www.example.com.`) and `@` for the zone apex all work and " +
					"mean the same thing; `fqdn` shows what it resolved to.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Record type: `A`, `AAAA`, `MX`, `TXT`, `CNAME`, `SRV`… " +
					"`SOA` and the apex `NS` set are managed by the platform and cannot be written.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"fqdn": schema.StringAttribute{
				MarkdownDescription: "The fully qualified owner name actually written, with its " +
					"trailing dot.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"ttl": schema.Int64Attribute{
				MarkdownDescription: fmt.Sprintf(
					"Time to live in seconds, shared by the whole set. Between %d and %d.",
					nubulus.MinTTL, nubulus.MaxTTL),
				Required: true,
				Validators: []validator.Int64{
					int64validator.Between(nubulus.MinTTL, nubulus.MaxTTL),
				},
			},
			"values": schema.SetAttribute{
				MarkdownDescription: "The record data, in presentation form (`\"10.0.0.1\"`, " +
					"`\"10 mail.example.com.\"`, `\"\\\"v=spf1 -all\\\"\"`). A set rather than a list " +
					"because a record set is a set: the order carries no meaning in the DNS and the API " +
					"sorts it, so a list would show a permanent diff.",
				ElementType: types.StringType,
				Required:    true,
				Validators: []validator.Set{
					setvalidator.SizeBetween(1, nubulus.MaxRRsetValues),
				},
			},
		},
	}
}

func (r *dnsRRsetResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

// ValidateConfig catches at plan time what the API would refuse at apply time.
//
// Every rule here is one the API enforces as well, and NONE of them is stricter
// than the service: a provider that refuses a configuration the platform would
// have accepted is worse than one that says nothing, because there is no way
// around it. The rdata itself is deliberately not validated — that needs a real
// DNS parser, and half a parser here would reject valid records.
func (r *dnsRRsetResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config dnsRRsetResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Anything unknown comes from another resource and cannot be checked now.
	if config.Zone.IsUnknown() || config.Name.IsUnknown() || config.Type.IsUnknown() {
		return
	}

	zone := config.Zone.ValueString()
	if _, ok := nubulus.NormalizeZoneName(zone); !ok {
		resp.Diagnostics.AddAttributeError(path.Root("zone"), "Invalid zone name",
			"`"+zone+"` is not a valid zone name.")
		return
	}

	fqdn, ok := nubulus.QualifyName(config.Name.ValueString(), zone)
	if !ok {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Record name outside the zone",
			"`"+config.Name.ValueString()+"` cannot be placed inside `"+zone+"`. Give the name "+
				"relative to the zone (`www`), fully qualified (`www."+zone+"`), or `@` for the apex.")
		return
	}

	rrtype := strings.ToUpper(strings.TrimSpace(config.Type.ValueString()))
	apex := nubulus.IsApex(fqdn, zone)

	if _, forbidden := nubulus.ForbiddenTypes[rrtype]; forbidden {
		resp.Diagnostics.AddAttributeError(path.Root("type"), "Record type cannot be managed",
			rrtype+" records cannot be written through the API. The DNSSEC types belong to the "+
				"name server when signing is on, and the rest are not records at all.")
		return
	}

	if _, managed := nubulus.ManagedAtApex[rrtype]; managed && (rrtype == "SOA" || apex) {
		resp.Diagnostics.AddAttributeError(path.Root("type"), "Record set managed by the platform",
			"The "+rrtype+" record set at the apex of `"+zone+"` is managed by Nubulus. Deleting or "+
				"rewriting it would take the zone off the internet. An NS set *below* the apex, "+
				"delegating a subzone, is yours and is allowed.")
		return
	}

	if rrtype == "CNAME" {
		if apex {
			resp.Diagnostics.AddAttributeError(path.Root("type"), "CNAME at the zone apex",
				"A CNAME cannot exist at the apex, where the SOA and NS sets have to live. This is "+
					"impossible in the DNS rather than merely discouraged.")
			return
		}
		if !config.Values.IsUnknown() && !config.Values.IsNull() && len(config.Values.Elements()) != 1 {
			resp.Diagnostics.AddAttributeError(path.Root("values"), "A CNAME holds exactly one value",
				"The whole meaning of a CNAME is that the name is an alias for one other name.")
			return
		}
	}

	// The API trims every value before writing it. A configured value with
	// surrounding whitespace would therefore come back different from what was
	// planned, and Terraform reports that as "Provider produced inconsistent
	// result after apply" — a confusing message for a stray space.
	if !config.Values.IsUnknown() && !config.Values.IsNull() {
		for _, element := range config.Values.Elements() {
			value, ok := element.(types.String)
			if !ok || value.IsUnknown() || value.IsNull() {
				continue
			}
			if raw := value.ValueString(); raw != strings.TrimSpace(raw) {
				resp.Diagnostics.AddAttributeError(path.Root("values"), "Value has surrounding whitespace",
					"`"+raw+"` is stored trimmed by the API, which would leave the plan and the result "+
						"disagreeing. Remove the leading or trailing spaces.")
				return
			}
		}
	}
}

func (r *dnsRRsetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsRRsetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	zone := plan.Zone.ValueString()
	fqdn, ok := nubulus.QualifyName(plan.Name.ValueString(), zone)
	if !ok {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Record name outside the zone",
			"`"+plan.Name.ValueString()+"` cannot be placed inside `"+zone+"`.")
		return
	}
	rrtype := strings.ToUpper(strings.TrimSpace(plan.Type.ValueString()))

	// PUT replaces whatever is there, so creating on top of an existing record
	// set would silently destroy records this configuration never mentioned —
	// records somebody may be relying on. Looking first turns that into an
	// error naming the import command.
	//
	// A failure of the LOOK is not fatal: it is usually the same problem the
	// write is about to report (a pending zone answers 409 here too), and the
	// write's error is the better one to show.
	existing, err := r.client.DNS.GetRRset(ctx, zone, fqdn, rrtype)
	switch {
	case err == nil && existing != nil:
		resp.Diagnostics.AddError(
			"The record set already exists",
			fmt.Sprintf("There is already a %s record set at %s in %s. Writing it would replace "+
				"values this configuration does not know about.\n\nImport it instead:\n\n"+
				"    terraform import nubuluscloud_dns_rrset.<name> %s",
				rrtype, fqdn, zone, importID(zone, fqdn, rrtype)),
		)
		return
	case err != nil && !nubulus.IsNotFound(err):
		tflogWarn(ctx, "could not check whether the record set already exists", err)
	}

	values, diags := stringSlice(ctx, plan.Values)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	set, err := r.client.DNS.PutRRset(ctx, zone, fqdn, rrtype, uint32(plan.TTL.ValueInt64()), values)
	if err != nil {
		addAPIError(&resp.Diagnostics, fmt.Sprintf("write the %s record set at %s", rrtype, fqdn), err)
		return
	}

	r.apply(ctx, &plan, zone, fqdn, rrtype, set, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsRRsetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsRRsetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	zone := state.Zone.ValueString()
	fqdn, ok := nubulus.QualifyName(state.Name.ValueString(), zone)
	if !ok {
		resp.State.RemoveResource(ctx)
		return
	}
	rrtype := strings.ToUpper(strings.TrimSpace(state.Type.ValueString()))

	set, err := r.client.DNS.GetRRset(ctx, zone, fqdn, rrtype)
	if err != nil {
		if nubulus.IsNotFound(err) {
			// Covers both the record set being gone and the zone being gone:
			// The API answers 404 for a zone that is not the caller's either
			// way, and in both cases the resource has to be created again.
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, fmt.Sprintf("read the %s record set at %s", rrtype, fqdn), err)
		return
	}

	r.apply(ctx, &state, zone, fqdn, rrtype, set, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is the same PUT as Create. Only ttl and values can reach it: zone,
// name and type replace the resource.
func (r *dnsRRsetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsRRsetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	zone := plan.Zone.ValueString()
	fqdn, ok := nubulus.QualifyName(plan.Name.ValueString(), zone)
	if !ok {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Record name outside the zone",
			"`"+plan.Name.ValueString()+"` cannot be placed inside `"+zone+"`.")
		return
	}
	rrtype := strings.ToUpper(strings.TrimSpace(plan.Type.ValueString()))

	values, diags := stringSlice(ctx, plan.Values)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	set, err := r.client.DNS.PutRRset(ctx, zone, fqdn, rrtype, uint32(plan.TTL.ValueInt64()), values)
	if err != nil {
		addAPIError(&resp.Diagnostics, fmt.Sprintf("update the %s record set at %s", rrtype, fqdn), err)
		return
	}

	r.apply(ctx, &plan, zone, fqdn, rrtype, set, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsRRsetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsRRsetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	zone := state.Zone.ValueString()
	fqdn, ok := nubulus.QualifyName(state.Name.ValueString(), zone)
	if !ok {
		return
	}
	rrtype := strings.ToUpper(strings.TrimSpace(state.Type.ValueString()))

	if err := r.client.DNS.DeleteRRset(ctx, zone, fqdn, rrtype); err != nil {
		if nubulus.IsNotFound(err) {
			return
		}
		addAPIError(&resp.Diagnostics, fmt.Sprintf("delete the %s record set at %s", rrtype, fqdn), err)
	}
}

// ImportState takes `<zone>/<name>/<type>`, which is also the id.
func (r *dnsRRsetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			"Expected `<zone>/<name>/<type>`, for example `example.com/www.example.com./A`. Got: "+req.ID,
		)
		return
	}

	zone, name, rrtype := parts[0], parts[1], strings.ToUpper(parts[2])
	if _, ok := nubulus.QualifyName(name, zone); !ok {
		resp.Diagnostics.AddError(
			"Record name outside the zone",
			"`"+name+"` cannot be placed inside `"+zone+"`.",
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone"), zone)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("type"), rrtype)...)
}

// apply copies an API answer over the model.
//
// It deliberately does NOT write back the name and the type: those keep the
// spelling the configuration used. Terraform requires the value it saves for a
// non-computed attribute to equal the one it planned, so replacing `www` with
// `www.example.com.` — the same record, spelled the way the API spells it —
// would fail the apply with "Provider produced inconsistent result".
func (r *dnsRRsetResource) apply(
	ctx context.Context,
	model *dnsRRsetResourceModel,
	zone, fqdn, rrtype string,
	set *nubulus.RRset,
	diags *diag.Diagnostics,
) {
	model.ID = types.StringValue(importID(zone, fqdn, rrtype))
	model.FQDN = types.StringValue(fqdn)

	if set == nil {
		return
	}

	model.TTL = types.Int64Value(int64(set.TTL))

	values, valueDiags := stringSet(set.Values)
	diags.Append(valueDiags...)
	model.Values = values
}

func importID(zone, fqdn, rrtype string) string {
	return zone + "/" + fqdn + "/" + rrtype
}
