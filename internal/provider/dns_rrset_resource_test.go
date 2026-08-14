package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccDNSRRset exercises the whole life of a record set against a real
// account: written, changed in place, imported.
//
// It needs NUBULUS_TEST_ZONE to name a zone the account already owns and is
// allowed to have records written into. The record it uses is scoped by the
// test name so a failed run leaves something recognisable behind rather than
// something that looks like production.
func TestAccDNSRRset(t *testing.T) {
	zone := testZone()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRRsetConfig(zone, 300, `["203.0.113.10", "203.0.113.11"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nubuluscloud_dns_rrset.test", "ttl", "300"),
					resource.TestCheckResourceAttr("nubuluscloud_dns_rrset.test", "values.#", "2"),
					// The relative name in the configuration has to come back
					// fully qualified here and unchanged in `name`.
					resource.TestCheckResourceAttr("nubuluscloud_dns_rrset.test", "name", "tf-acc-test"),
					resource.TestCheckResourceAttr("nubuluscloud_dns_rrset.test", "fqdn",
						fmt.Sprintf("tf-acc-test.%s.", zone)),
				),
			},
			{
				// An update: same set, different TTL and one value fewer. It
				// must NOT replace the resource.
				Config: testAccRRsetConfig(zone, 600, `["203.0.113.10"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nubuluscloud_dns_rrset.test", "ttl", "600"),
					resource.TestCheckResourceAttr("nubuluscloud_dns_rrset.test", "values.#", "1"),
				),
			},
			{
				ResourceName:      "nubuluscloud_dns_rrset.test",
				ImportState:       true,
				ImportStateId:     fmt.Sprintf("%s/tf-acc-test.%s./A", zone, zone),
				ImportStateVerify: true,
				// `name` survives import in the API's spelling rather than the
				// configuration's, which is the one difference import cannot
				// avoid: the platform does not record how it was written.
				ImportStateVerifyIgnore: []string{"name"},
			},
		},
	})
}

func testAccRRsetConfig(zone string, ttl int, values string) string {
	return fmt.Sprintf(`
resource "nubuluscloud_dns_rrset" "test" {
  zone   = %[1]q
  name   = "tf-acc-test"
  type   = "A"
  ttl    = %[2]d
  values = %[3]s
}
`, zone, ttl, values)
}
