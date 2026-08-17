package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

// Ensure the provider satisfies the framework interface.
var _ provider.Provider = &NubulusProvider{}

// NubulusProvider is the provider implementation.
type NubulusProvider struct {
	// version is the provider version on release, "dev" when built locally and
	// "test" during acceptance tests.
	version string
}

// NubulusProviderModel is the provider block.
//
// Every field here needs a matching attribute in the schema below: a `tfsdk`
// tag with nothing to read into it fails when the configuration is read, on the
// first real command. TestProviderSchemaAndModelAgree is what keeps the two in
// step.
type NubulusProviderModel struct {
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &NubulusProvider{version: version}
	}
}

func (p *NubulusProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "nubuluscloud"
	resp.Version = p.version
}

func (p *NubulusProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages Nubulus Cloud resources. Authentication uses an " +
			"**application token**, created from the panel at `/dashboard/account/tokens`, " +
			"which is a machine credential belonging to the account rather than to a person.",

		Attributes: map[string]schema.Attribute{
			"client_id": schema.StringAttribute{
				MarkdownDescription: "Client id of the application token. May also be set with the " +
					"`NUBULUS_CLIENT_ID` environment variable.",
				Optional: true,
			},
			"client_secret": schema.StringAttribute{
				MarkdownDescription: "Client secret of the application token. May also be set with the " +
					"`NUBULUS_CLIENT_SECRET` environment variable, which is the recommended way: the " +
					"secret is shown once, at creation, and is enough on its own to act as the account.",
				Optional:  true,
				Sensitive: true,
			},
		},
	}
}

func (p *NubulusProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config NubulusProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// An unknown here means the value comes from another resource's output, so
	// the provider cannot be configured during plan. Saying which attribute it
	// was is the difference between a fixable message and a puzzle.
	for name, value := range map[string]types.String{
		"client_id":     config.ClientID,
		"client_secret": config.ClientSecret,
	} {
		if value.IsUnknown() {
			resp.Diagnostics.AddAttributeError(
				path.Root(name),
				"Provider configuration is not known at plan time",
				"The provider cannot be configured with a value that is only known after apply. "+
					"Set "+name+" statically, from a variable, or from the environment.",
			)
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := clientConfig(config, p.version)

	if cfg.ClientID == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("client_id"),
			"Missing client id",
			"Set `client_id` in the provider block or the NUBULUS_CLIENT_ID environment variable. "+
				"Both halves come from the application token created at /dashboard/account/tokens.",
		)
	}
	if cfg.ClientSecret == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("client_secret"),
			"Missing client secret",
			"Set `client_secret` in the provider block or the NUBULUS_CLIENT_SECRET environment "+
				"variable. The secret is only ever shown when the token is created; if it was lost, "+
				"revoke the token and create another one.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	client, err := nubulus.New(ctx, cfg)
	if err != nil {
		resp.Diagnostics.AddError("Could not build the API client", err.Error())
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *NubulusProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewDNSZoneResource,
		NewDNSZoneVerificationResource,
		NewDNSRRsetResource,
		NewTunnelResource,
		NewTunnelRouteResource,
	}
}

func (p *NubulusProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDNSZoneDataSource,
		NewDNSZonesDataSource,
		NewTunnelsDataSource,
	}
}

// clientConfig assembles what the client is built from: the credential, which
// the configuration may carry, and the platform values, which it may not.
//
// The token endpoint, the project and the two API endpoints describe a hosted
// service rather than a preference of whoever writes the configuration — there
// is one production, and no other value for them is useful against it. So they
// are not attributes of the provider block. That matters most for the token
// endpoint, which is where the client id and secret are sent: a configuration
// file cannot redirect them anywhere.
//
// They are still read from the environment, which keeps a build usable against
// a test or staging environment without cutting a release, and the compiled-in
// defaults in nubulus.New fill in whatever arrives empty. If one of them has to
// change for good, that is a new version of the provider.
func clientConfig(config NubulusProviderModel, version string) nubulus.Config {
	return nubulus.Config{
		ClientID:       firstSet(config.ClientID, "NUBULUS_CLIENT_ID"),
		ClientSecret:   firstSet(config.ClientSecret, "NUBULUS_CLIENT_SECRET"),
		TokenURL:       os.Getenv("NUBULUS_TOKEN_URL"),
		ProjectID:      os.Getenv("NUBULUS_PROJECT_ID"),
		DNSEndpoint:    os.Getenv("NUBULUS_DNS_ENDPOINT"),
		TunnelEndpoint: os.Getenv("NUBULUS_TUNNEL_ENDPOINT"),
		UserAgent:      "terraform-provider-nubuluscloud/" + version,
	}
}

// firstSet prefers the configured value and falls back to the environment.
//
// That order rather than the reverse: a value written in the configuration is
// the one the person reading the configuration expects to win, and an
// environment variable that silently overrode it would be invisible in every
// review of that file.
func firstSet(configured types.String, envVar string) string {
	if !configured.IsNull() && configured.ValueString() != "" {
		return configured.ValueString()
	}
	return os.Getenv(envVar)
}
