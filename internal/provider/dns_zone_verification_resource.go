package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

var (
	_ resource.Resource              = &dnsZoneVerificationResource{}
	_ resource.ResourceWithConfigure = &dnsZoneVerificationResource{}
)

// The defaults of the wait. They are generous on purpose: see the comment on
// Create.
const (
	defaultVerificationTimeout = 90 * time.Minute
	defaultVerificationPoll    = 30 * time.Second
)

func NewDNSZoneVerificationResource() resource.Resource {
	return &dnsZoneVerificationResource{}
}

type dnsZoneVerificationResource struct {
	client *nubulus.Client
}

type dnsZoneVerificationResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Zone         types.String `tfsdk:"zone"`
	Timeout      types.String `tfsdk:"timeout"`
	PollInterval types.String `tfsdk:"poll_interval"`

	Method     types.String `tfsdk:"method"`
	VerifiedAt types.String `tfsdk:"verified_at"`
}

func (r *dnsZoneVerificationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone_verification"
}

func (r *dnsZoneVerificationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Waits until control of a zone's name has been proven, and only then " +
			"considers itself created.\n\n" +
			"It manages nothing of its own: it exists so that record resources can depend on the " +
			"proof rather than on the claim. A zone claimed for a name registered elsewhere does not " +
			"exist on the name servers until it is verified, and every write into it fails until " +
			"then.\n\n" +
			"Put the challenge record where the domain resolves **today**, then make this resource " +
			"depend on it:\n\n" +
			"```terraform\n" +
			"resource \"nubuluscloud_dns_zone\" \"main\" {\n" +
			"  name = \"example.com\"\n" +
			"}\n\n" +
			"resource \"nubuluscloud_dns_zone_verification\" \"main\" {\n" +
			"  zone       = nubuluscloud_dns_zone.main.name\n" +
			"  depends_on = [<the TXT record at your current DNS provider>]\n" +
			"}\n\n" +
			"resource \"nubuluscloud_dns_rrset\" \"www\" {\n" +
			"  zone       = nubuluscloud_dns_zone.main.name\n" +
			"  name       = \"www\"\n" +
			"  type       = \"A\"\n" +
			"  ttl        = 300\n" +
			"  values     = [\"203.0.113.10\"]\n" +
			"  depends_on = [nubuluscloud_dns_zone_verification.main]\n" +
			"}\n" +
			"```\n\n" +
			"A zone whose name was already assigned to the account is verified from the moment it is " +
			"created, so this resource succeeds on its first attempt and the same module works for " +
			"both kinds of name.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The zone name.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone": schema.StringAttribute{
				MarkdownDescription: "Zone to verify.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"timeout": schema.StringAttribute{
				MarkdownDescription: "How long to keep trying, as a Go duration. Defaults to `90m`, " +
					"which is deliberately generous: the usual reason an attempt fails is that the " +
					"parent zone is still serving the cached NXDOMAIN from before the challenge record " +
					"was published, and that cache commonly lasts an hour.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("90m"),
			},
			"poll_interval": schema.StringAttribute{
				MarkdownDescription: "How long to wait between attempts, as a Go duration. Defaults " +
					"to `30s`.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("30s"),
			},
			"method": schema.StringAttribute{
				MarkdownDescription: "How control was proven: `txt` for the challenge record, `ns` " +
					"when the parent zone already delegates the name to the Nubulus name servers.",
				Computed: true,
			},
			"verified_at": schema.StringAttribute{
				MarkdownDescription: "When the proof landed, in RFC 3339.",
				Computed:            true,
			},
		},
	}
}

func (r *dnsZoneVerificationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

// Create asks for the proof until it lands or the timeout runs out.
//
// A FAILED ATTEMPT IS A 200 WITH verified:false, not an error, and the loop is
// written against that: an attempt that has not succeeded yet is the normal
// answer to a perfectly good request. The reason code is what separates
// "published it thirty seconds ago, wait" from "published the wrong value",
// which are the two mistakes people actually make, and it is carried into the
// final diagnostic so the difference survives the timeout.
func (r *dnsZoneVerificationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsZoneVerificationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, ok := duration(plan.Timeout, defaultVerificationTimeout, path.Root("timeout"), &resp.Diagnostics)
	if !ok {
		return
	}
	poll, ok := duration(plan.PollInterval, defaultVerificationPoll, path.Root("poll_interval"), &resp.Diagnostics)
	if !ok {
		return
	}

	zone := plan.Zone.ValueString()
	deadline := time.Now().Add(timeout)

	var last *nubulus.VerificationResult
	for {
		result, err := r.client.DNS.Verify(ctx, zone)
		if err != nil {
			addAPIError(&resp.Diagnostics, "verify the zone "+zone, err)
			return
		}
		last = result

		if result.Verified {
			plan.ID = types.StringValue(zone)
			plan.Method = stringValue(result.Method)
			plan.VerifiedAt = types.StringValue(result.CheckedAt.Format(rfc3339))
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			return
		}

		tflog.Info(ctx, "zone not verified yet, waiting", map[string]any{
			"zone":        zone,
			"reason_code": result.ReasonCode,
			"reason":      result.Reason,
		})

		if time.Now().Add(poll).After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			resp.Diagnostics.AddError("Verification was interrupted",
				"Waiting for "+zone+" to be verified was cancelled: "+ctx.Err().Error())
			return
		case <-time.After(poll):
		}
	}

	resp.Diagnostics.AddError(
		"The zone "+zone+" was not verified in time",
		verificationAdvice(last, timeout),
	)
}

// Read drops the resource when the zone stops being verified — which is to say
// when the zone is gone, or has been reclaimed and is pending again. In both
// cases the proof has to happen once more, and that is exactly what recreating
// this resource does.
func (r *dnsZoneVerificationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsZoneVerificationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	zone := state.Zone.ValueString()
	detail, err := r.client.DNS.GetZone(ctx, zone)
	if err != nil {
		if nubulus.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "read the zone "+zone, err)
		return
	}

	if detail.Zone == nil || detail.Zone.Status == "pending_verification" {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(zone)
	if detail.Zone.VerifiedAt != nil {
		state.VerifiedAt = timeValue(detail.Zone.VerifiedAt)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only ever sees timeout and poll_interval change — zone replaces the
// resource — so there is nothing to do but keep what is already known.
func (r *dnsZoneVerificationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dnsZoneVerificationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID
	plan.Method = state.Method
	plan.VerifiedAt = state.VerifiedAt
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete does nothing, and that is the whole point: destroying this resource
// must not un-verify a zone. Removing the proof is what deleting the zone does.
func (r *dnsZoneVerificationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
}

// verificationAdvice turns the last attempt into something worth reading.
func verificationAdvice(last *nubulus.VerificationResult, timeout time.Duration) string {
	if last == nil {
		return "No attempt returned an answer within " + timeout.String() + "."
	}

	detail := "The last attempt, after " + timeout.String() + ", answered: " + last.Reason +
		" (" + last.ReasonCode + ").\n\n"

	switch last.ReasonCode {
	case "TXT_NOT_FOUND":
		return detail + "No challenge record was found at all. Either it has not been published yet, " +
			"or the negative cache of the parent zone is still answering with what was true before it " +
			"was — that cache lasts as long as the parent's SOA minimum, commonly an hour. Check the " +
			"record is published, then raise `timeout`."
	case "TXT_MISMATCH":
		return detail + "A challenge record exists but none of its values is this zone's token. This " +
			"one is worth acting on now: copy `verification_txt_value` from the zone resource exactly."
	case "NS_MISMATCH":
		return detail + "The parent zone delegates this name somewhere else, which is reported " +
			"alongside the TXT attempt and never on its own."
	case "LOOKUP_FAILED":
		return detail + "The resolver could not answer at all. That is a problem on our side or in " +
			"the network, not with the record: retrying later is the right move."
	}
	return detail
}

// duration parses a Go duration attribute, falling back to a default when it is
// null or unknown.
func duration(value types.String, fallback time.Duration, at path.Path, diags *diag.Diagnostics) (time.Duration, bool) {
	if value.IsNull() || value.IsUnknown() || value.ValueString() == "" {
		return fallback, true
	}
	parsed, err := time.ParseDuration(value.ValueString())
	if err != nil {
		diags.AddAttributeError(at, "Invalid duration",
			"`"+value.ValueString()+"` is not a duration: "+err.Error()+". Use a Go duration such as "+
				"`30s`, `10m` or `2h`.")
		return 0, false
	}
	if parsed <= 0 {
		diags.AddAttributeError(at, "Invalid duration", "The duration must be positive.")
		return 0, false
	}
	return parsed, true
}
