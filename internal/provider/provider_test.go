package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

// testAccProtoV6ProviderFactories instantiates the provider for acceptance
// tests. The factory is called once per Terraform CLI command.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"nubuluscloud": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck refuses to run acceptance tests without real credentials.
//
// These tests create real zones and real records against a real account, and
// the failure mode of forgetting that is a confusing 401 halfway through a test
// run. NUBULUS_TEST_ZONE has to be a zone the account already owns and may have
// records written into: the record tests write into it and clean up after
// themselves.
func testAccPreCheck(t *testing.T) {
	t.Helper()

	for _, name := range []string{"NUBULUS_CLIENT_ID", "NUBULUS_CLIENT_SECRET", "NUBULUS_TEST_ZONE"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s must be set for acceptance tests", name)
		}
	}
}

// testZone is the zone the record acceptance tests operate in.
func testZone() string { return os.Getenv("NUBULUS_TEST_ZONE") }

// providerSchema builds the provider schema, failing the test rather than
// returning a half-built one.
func providerSchema(t *testing.T) schema.Schema {
	t.Helper()

	var resp provider.SchemaResponse
	New("test")().Schema(t.Context(), provider.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// The provider block takes the credential and nothing else. The token endpoint,
// the project and the API endpoints are properties of a hosted service, so they
// are read from the environment and never from a configuration.
//
// This test is the thing that stops one of them being added back as an
// attribute without the decision being made again on purpose.
func TestProviderSchemaTakesOnlyTheCredential(t *testing.T) {
	want := map[string]bool{"client_id": true, "client_secret": true}

	attributes := providerSchema(t).Attributes
	for name := range attributes {
		if !want[name] {
			t.Errorf("%q must not be an attribute of the provider block: it is not something a "+
				"configuration gets to choose. Read it from the environment instead.", name)
		}
	}
	for name := range want {
		if _, ok := attributes[name]; !ok {
			t.Errorf("%q is missing from the provider schema", name)
		}
	}
}

// The schema and the model have to agree, and nothing at compile time makes
// them: an attribute with no matching `tfsdk` field fails at runtime, on the
// first real command, with a value conversion error that names neither. The
// other direction is worse — a `tfsdk` field with no attribute behind it fails
// the same way, which is exactly what removing an attribute risks.
//
// So the test round-trips a whole configuration through the model, covering
// every attribute rather than one of them.
func TestProviderSchemaAndModelAgree(t *testing.T) {
	ctx := t.Context()
	providerSchema := providerSchema(t)

	objectType, ok := providerSchema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("a provider schema is an object, got %T", providerSchema.Type().TerraformType(ctx))
	}

	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(tftypes.String, name+"-value")
	}

	config := tfsdk.Config{
		Schema: providerSchema,
		Raw:    tftypes.NewValue(objectType, values),
	}

	var model NubulusProviderModel
	if diags := config.Get(ctx, &model); diags.HasError() {
		t.Fatalf("reading the configuration into the model: %v", diags)
	}
	if model.ClientID.ValueString() != "client_id-value" {
		t.Errorf("ClientID = %q", model.ClientID.ValueString())
	}
	if model.ClientSecret.ValueString() != "client_secret-value" {
		t.Errorf("ClientSecret = %q", model.ClientSecret.ValueString())
	}
}

// The credential may come from the environment, and a value written in the
// configuration wins over one that is only in the environment.
func TestTheCredentialComesFromTheEnvironment(t *testing.T) {
	t.Setenv("NUBULUS_CLIENT_SECRET", "from-the-environment")

	if got := firstSet(types.StringNull(), "NUBULUS_CLIENT_SECRET"); got != "from-the-environment" {
		t.Errorf("with nothing configured, got %q", got)
	}
	if got := firstSet(types.StringValue("configured"), "NUBULUS_CLIENT_SECRET"); got != "configured" {
		t.Errorf("the configured value must win, got %q", got)
	}
}

// The four platform values left the schema but not the provider: they are still
// read from the environment, which is what keeps a build usable against a test
// environment without a release. Losing that would be silent — the compiled-in
// defaults would answer instead, and every request would go to production.
func TestPlatformValuesComeFromTheEnvironment(t *testing.T) {
	t.Setenv("NUBULUS_TOKEN_URL", "https://token.example/oauth2/token")
	t.Setenv("NUBULUS_PROJECT_ID", "a-project")
	t.Setenv("NUBULUS_DNS_ENDPOINT", "https://dns.example")
	t.Setenv("NUBULUS_TUNNEL_ENDPOINT", "https://tunnel.example")

	cfg := clientConfig(NubulusProviderModel{}, "test")

	for _, tc := range []struct{ name, got, want string }{
		{"TokenURL", cfg.TokenURL, "https://token.example/oauth2/token"},
		{"ProjectID", cfg.ProjectID, "a-project"},
		{"DNSEndpoint", cfg.DNSEndpoint, "https://dns.example"},
		{"TunnelEndpoint", cfg.TunnelEndpoint, "https://tunnel.example"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// With nothing in the environment the four arrive empty, which is what makes
// nubulus.New apply the compiled-in defaults. An empty string reaching the
// client instead would be a base URL of "" and a request to a relative path.
func TestPlatformValuesAreLeftToTheCompiledDefaults(t *testing.T) {
	for _, name := range []string{
		"NUBULUS_TOKEN_URL", "NUBULUS_PROJECT_ID",
		"NUBULUS_DNS_ENDPOINT", "NUBULUS_TUNNEL_ENDPOINT",
	} {
		t.Setenv(name, "")
	}

	cfg := clientConfig(NubulusProviderModel{
		ClientID:     types.StringValue("an-id"),
		ClientSecret: types.StringValue("a-secret"),
	}, "test")
	if cfg.TokenURL != "" || cfg.ProjectID != "" || cfg.DNSEndpoint != "" || cfg.TunnelEndpoint != "" {
		t.Fatalf("expected the four to be empty so nubulus.New fills them in, got %+v", cfg)
	}

	client, err := nubulus.New(t.Context(), cfg)
	if err != nil {
		t.Fatalf("nubulus.New: %v", err)
	}
	if client.DNS == nil || client.Tunnel == nil {
		t.Error("both typed clients must be built")
	}
}
