package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
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
