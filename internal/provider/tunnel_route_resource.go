package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

var (
	_ resource.Resource                   = &tunnelRouteResource{}
	_ resource.ResourceWithImportState    = &tunnelRouteResource{}
	_ resource.ResourceWithConfigure      = &tunnelRouteResource{}
	_ resource.ResourceWithValidateConfig = &tunnelRouteResource{}
)

func NewTunnelRouteResource() resource.Resource {
	return &tunnelRouteResource{}
}

type tunnelRouteResource struct {
	client *nubulus.Client
}

type tunnelRouteResourceModel struct {
	ID       types.String `tfsdk:"id"`
	TunnelID types.String `tfsdk:"tunnel_id"`

	Type       types.String `tfsdk:"type"`
	Hostname   types.String `tfsdk:"hostname"`
	PathPrefix types.String `tfsdk:"path_prefix"`

	UpstreamHost   types.String `tfsdk:"upstream_host"`
	UpstreamPort   types.Int64  `tfsdk:"upstream_port"`
	UpstreamScheme types.String `tfsdk:"upstream_scheme"`
	StripPrefix    types.Bool   `tfsdk:"strip_prefix"`

	Enabled  types.Bool  `tfsdk:"enabled"`
	Priority types.Int64 `tfsdk:"priority"`
}

func (r *tunnelRouteResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tunnel_route"
}

func (r *tunnelRouteResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Sends requests for a hostname, optionally only those under a path, " +
			"through a tunnel to an upstream reachable from the machine the tunnel client runs on.\n\n" +
			"Point the hostname at the tunnel's `cname_target` with a CNAME, or nothing will arrive.\n\n" +
			"`hostname` is unique across the whole platform, not just your account: if another " +
			"account is already serving it, creating this fails and there is nothing you can inspect " +
			"or change to fix it from your side.\n\n" +
			"Routes live inside an `active` tunnel. Writing one into a tunnel that is not active " +
			"fails until the tunnel itself changes.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Route identifier, assigned by the platform.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"tunnel_id": schema.StringAttribute{
				MarkdownDescription: "The tunnel this route belongs to. Changing it replaces the route.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},

			// The three below replace the route because the API has no way to
			// edit them: its update accepts only the upstream, strip_prefix,
			// priority and enabled. Declaring them updatable would produce a
			// plan promising to change a hostname and an apply that changes
			// nothing, a provider lying about what it did.
			"type": schema.StringAttribute{
				MarkdownDescription: "`host` matches every request for the hostname; `path` matches " +
					"only those under `path_prefix`. Changing it replaces the route.",
				Required:      true,
				Validators:    []validator.String{stringvalidator.OneOf("host", "path")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"hostname": schema.StringAttribute{
				MarkdownDescription: "The fully qualified name to serve. Unique across the platform. " +
					"Changing it replaces the route.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"path_prefix": schema.StringAttribute{
				MarkdownDescription: "Path this route matches under. `/` for a `host` route; for a " +
					"`path` route it is required and must not be `/`. Changing it replaces the route.",
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("/"),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},

			"upstream_host": schema.StringAttribute{
				MarkdownDescription: "Host or address the request is sent to, as seen from the " +
					"machine running the tunnel client.",
				Required: true,
			},
			"upstream_port": schema.Int64Attribute{
				MarkdownDescription: "Port on the upstream, 1 to 65535.",
				Required:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 65535)},
			},
			"upstream_scheme": schema.StringAttribute{
				MarkdownDescription: "`http` or `https`, how to talk to the upstream.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("http"),
				Validators:          []validator.String{stringvalidator.OneOf("http", "https")},
			},
			"strip_prefix": schema.BoolAttribute{
				MarkdownDescription: "Remove `path_prefix` from the path before the request reaches " +
					"the upstream.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},

			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the route serves traffic. Disabling one keeps it and " +
					"its hostname reserved without answering anything.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"priority": schema.Int64Attribute{
				MarkdownDescription: "Orders the routes of a tunnel when more than one could match: " +
					"**lower wins**.",
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(100),
			},
		},
	}
}

// ValidateConfig catches the one rule that cannot be expressed on a single
// attribute, at plan time rather than as a 400 halfway through an apply.
func (r *tunnelRouteResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config tunnelRouteResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Type.IsUnknown() || config.Type.ValueString() != "path" {
		return
	}

	// Null here means the default will make it "/", so the two cases are the
	// same mistake and get the same message.
	prefix := config.PathPrefix
	if prefix.IsUnknown() {
		return
	}
	if prefix.IsNull() || prefix.ValueString() == "/" {
		resp.Diagnostics.AddAttributeError(path.Root("path_prefix"),
			"A path route needs a path prefix",
			"`type = \"path\"` matches requests under a prefix, so the prefix has to be something "+
				"other than `/`. Leaving it out defaults it to `/`, which would match everything "+
				"and is what `type = \"host\"` is for.")
		return
	}
	if !strings.HasPrefix(prefix.ValueString(), "/") {
		resp.Diagnostics.AddAttributeError(path.Root("path_prefix"),
			"The path prefix has to start with a slash",
			"`"+prefix.ValueString()+"` does not start with `/`.")
	}
}

func (r *tunnelRouteResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (r *tunnelRouteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan tunnelRouteResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tunnelID := plan.TunnelID.ValueString()
	created, err := r.client.Tunnel.CreateRoute(ctx, tunnelID, nubulus.CreateRouteInput{
		Type:           plan.Type.ValueString(),
		Hostname:       plan.Hostname.ValueString(),
		PathPrefix:     plan.PathPrefix.ValueString(),
		UpstreamHost:   plan.UpstreamHost.ValueString(),
		UpstreamPort:   int(plan.UpstreamPort.ValueInt64()),
		UpstreamScheme: plan.UpstreamScheme.ValueString(),
		StripPrefix:    plan.StripPrefix.ValueBool(),
		Priority:       int(plan.Priority.ValueInt64()),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "create the route for "+plan.Hostname.ValueString(), err)
		return
	}

	// TWO FIELDS CANNOT BE SET WHEN A ROUTE IS CREATED, so a create alone can
	// come back disagreeing with the plan, which Terraform reports as
	// "Provider produced inconsistent result after apply" and which, if it were
	// smoothed over by writing the server's answer instead, would show up as a
	// diff that never goes away:
	//
	//   - `enabled` is not in the create at all; a new route is always on.
	//   - `priority` is sent, but the API reads zero as "unset" and turns it
	//     into 100. Only the update distinguishes them, because there it
	//     arrives as a pointer.
	//
	// So the create is followed by an update whenever it landed somewhere other
	// than where the plan asked. In the ordinary case (enabled, priority not
	// zero) nothing extra is sent.
	route := created
	if fix, needed := routeCorrection(&plan, created); needed {
		updated, err := r.client.Tunnel.UpdateRoute(ctx, tunnelID, created.ID, fix)
		if err != nil {
			addAPIError(&resp.Diagnostics,
				"apply enabled/priority to the route just created ("+created.ID+")", err)
			return
		}
		route = updated
	}

	applyRoute(&plan, route)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// routeCorrection is the update a freshly created route needs, if any.
func routeCorrection(plan *tunnelRouteResourceModel, created *nubulus.Route) (nubulus.UpdateRouteInput, bool) {
	var fix nubulus.UpdateRouteInput
	needed := false

	if wanted := plan.Enabled.ValueBool(); wanted != created.Enabled {
		fix.Enabled = &wanted
		needed = true
	}
	if wanted := int(plan.Priority.ValueInt64()); wanted != created.Priority {
		fix.Priority = &wanted
		needed = true
	}

	return fix, needed
}

func (r *tunnelRouteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state tunnelRouteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tunnelID := state.TunnelID.ValueString()
	route, err := r.client.Tunnel.GetRoute(ctx, tunnelID, state.ID.ValueString())
	if err != nil {
		if nubulus.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "read the route "+state.ID.ValueString(), err)
		return
	}

	applyRoute(&state, route)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *tunnelRouteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan tunnelRouteResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Everything the API can change is sent every time. Sending only what
	// differs would need the prior state to compare against and buys nothing:
	// the update is a single request either way.
	host := plan.UpstreamHost.ValueString()
	port := int(plan.UpstreamPort.ValueInt64())
	scheme := plan.UpstreamScheme.ValueString()
	strip := plan.StripPrefix.ValueBool()
	priority := int(plan.Priority.ValueInt64())
	enabled := plan.Enabled.ValueBool()

	route, err := r.client.Tunnel.UpdateRoute(ctx, plan.TunnelID.ValueString(), plan.ID.ValueString(),
		nubulus.UpdateRouteInput{
			UpstreamHost:   &host,
			UpstreamPort:   &port,
			UpstreamScheme: &scheme,
			StripPrefix:    &strip,
			Priority:       &priority,
			Enabled:        &enabled,
		})
	if err != nil {
		addAPIError(&resp.Diagnostics, "update the route "+plan.ID.ValueString(), err)
		return
	}

	applyRoute(&plan, route)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tunnelRouteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state tunnelRouteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Tunnel.DeleteRoute(ctx, state.TunnelID.ValueString(), state.ID.ValueString())
	if err != nil && !nubulus.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete the route "+state.ID.ValueString(), err)
	}
}

// ImportState takes `<tunnel_id>/<route_id>`.
//
// Both halves are needed because a route is only addressable through its
// tunnel: there is no route endpoint that does not name one. Unlike a tunnel,
// this import is complete: a route has no write-only fields.
func (r *tunnelRouteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	tunnelID, routeID, ok := strings.Cut(req.ID, "/")
	if !ok || tunnelID == "" || routeID == "" {
		resp.Diagnostics.AddError(
			"Malformed import identifier",
			"A route is imported as `<tunnel_id>/<route_id>`, because a route can only be "+
				"addressed through its tunnel. Got: "+req.ID,
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("tunnel_id"), tunnelID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), routeID)...)
}

// applyRoute copies an API answer over the model. Every field of a route is
// readable, so unlike a tunnel there is nothing here that has to be preserved
// from the previous state.
func applyRoute(model *tunnelRouteResourceModel, route *nubulus.Route) {
	if route == nil {
		return
	}

	model.ID = types.StringValue(route.ID)
	model.TunnelID = types.StringValue(route.TunnelID)
	model.Type = types.StringValue(route.Type)
	model.Hostname = types.StringValue(route.Hostname)
	model.PathPrefix = types.StringValue(route.PathPrefix)
	model.UpstreamHost = types.StringValue(route.UpstreamHost)
	model.UpstreamPort = types.Int64Value(int64(route.UpstreamPort))
	model.UpstreamScheme = types.StringValue(route.UpstreamScheme)
	model.StripPrefix = types.BoolValue(route.StripPrefix)
	model.Enabled = types.BoolValue(route.Enabled)
	model.Priority = types.Int64Value(int64(route.Priority))
}
