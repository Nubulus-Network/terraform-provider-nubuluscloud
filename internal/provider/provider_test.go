package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
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

// The schema and the model have to agree, and nothing at compile time makes
// them: an attribute with no matching `tfsdk` field fails at runtime, on the
// first real command, with a value conversion error that names neither.
//
// So the test round-trips a whole configuration through the model. It covers
// every attribute rather than the new one alone, which is what makes it worth
// keeping.
func TestProviderSchemaAndModelAgree(t *testing.T) {
	ctx := t.Context()

	var schemaResp provider.SchemaResponse
	New("test")().Schema(ctx, provider.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("Schema: %v", schemaResp.Diagnostics)
	}

	if _, ok := schemaResp.Schema.Attributes["tunnel_endpoint"]; !ok {
		t.Fatal("tunnel_endpoint is not in the provider schema")
	}

	objectType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("a provider schema is an object, got %T", schemaResp.Schema.Type().TerraformType(ctx))
	}

	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(tftypes.String, name+"-value")
	}

	config := tfsdk.Config{
		Schema: schemaResp.Schema,
		Raw:    tftypes.NewValue(objectType, values),
	}

	var model NubulusProviderModel
	if diags := config.Get(ctx, &model); diags.HasError() {
		t.Fatalf("reading the configuration into the model: %v", diags)
	}
	if model.TunnelEndpoint.ValueString() != "tunnel_endpoint-value" {
		t.Errorf("TunnelEndpoint = %q", model.TunnelEndpoint.ValueString())
	}
	if model.DNSEndpoint.ValueString() != "dns_endpoint-value" {
		t.Errorf("DNSEndpoint = %q", model.DNSEndpoint.ValueString())
	}
}

// Every endpoint may come from the environment, and a value written in the
// configuration wins over one that is only in the environment.
func TestTunnelEndpointComesFromTheEnvironment(t *testing.T) {
	t.Setenv("NUBULUS_TUNNEL_ENDPOINT", "https://from-the-environment.example")

	if got := firstSet(types.StringNull(), "NUBULUS_TUNNEL_ENDPOINT"); got != "https://from-the-environment.example" {
		t.Errorf("with nothing configured, got %q", got)
	}
	if got := firstSet(types.StringValue("https://configured.example"), "NUBULUS_TUNNEL_ENDPOINT"); got != "https://configured.example" {
		t.Errorf("the configured value must win, got %q", got)
	}
}
