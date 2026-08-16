package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

func tunnelSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	NewTunnelResource().(*tunnelResource).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("building the schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// A read cannot see the credentials — the platform issues them once — so
// applying one must leave them exactly as they were.
//
// This is the failure that would be worst and quietest: the tunnel keeps
// working, the state loses the only copy of its credential, and nobody notices
// until the client has to be reconfigured and there is nothing to reconfigure
// it with.
func TestAReadNeverBlanksTheCredentials(t *testing.T) {
	state := &tunnelResourceModel{
		ID:                  types.StringValue("tun-1"),
		TunnelToken:         types.StringValue("tok-secret"),
		WireGuardPrivateKey: types.StringValue("private-secret"),
		WireGuardAddress:    types.StringValue("10.0.0.7/32"),
		PeerEndpoint:        types.StringValue("gw.example.net:51820"),
		CNAMETarget:         types.StringValue("tun-1.example.net"),
	}

	// What a read answers: no token, no private key, none of the peer block.
	applyTunnelRead(state, &nubulus.Tunnel{
		ID: "tun-1", AccountID: "acct_01",
		TunnelSubdomain: "tun-1.example.net", WireGuardIP: "10.0.0.7",
		WireGuardPublicKey: "client-public",
		Status:             "active", OnlineStatus: "online",
	})

	if state.TunnelToken.ValueString() != "tok-secret" {
		t.Errorf("tunnel_token = %q; a refresh destroyed the credential", state.TunnelToken.ValueString())
	}
	if state.WireGuardPrivateKey.ValueString() != "private-secret" {
		t.Errorf("wireguard_private_key = %q", state.WireGuardPrivateKey.ValueString())
	}
	if state.WireGuardAddress.ValueString() != "10.0.0.7/32" {
		t.Errorf("wireguard_address = %q", state.WireGuardAddress.ValueString())
	}
	if state.PeerEndpoint.ValueString() != "gw.example.net:51820" {
		t.Errorf("peer_endpoint = %q", state.PeerEndpoint.ValueString())
	}

	// And the things a read does know are refreshed.
	if state.OnlineStatus.ValueString() != "online" || state.Status.ValueString() != "active" {
		t.Errorf("the lifecycle was not refreshed: %+v", state)
	}
	if state.WireGuardPublicKey.ValueString() != "client-public" {
		t.Error("the public key does come back on a read and should be adopted")
	}
}

// An imported tunnel starts with nothing but its id, so a read has to fill in
// the identity the platform holds. A configured one must not be touched: the
// configuration is the truth there, and writing over it produces a permanent
// diff.
func TestAReadAdoptsIdentityOnlyWhenStateHasNone(t *testing.T) {
	imported := &tunnelResourceModel{ID: types.StringValue("tun-1")}
	applyTunnelRead(imported, &nubulus.Tunnel{
		ID: "tun-1", Name: "production", ExternalID: "cr-uid-1",
		TunnelSubdomain: "tun-1.example.net",
	})
	if imported.Name.ValueString() != "production" || imported.ExternalID.ValueString() != "cr-uid-1" {
		t.Errorf("an import did not pick up the identity: %+v", imported)
	}
	// cname_target is only in the create answer, but it is always the tunnel's
	// own subdomain, so an import can still have a correct one.
	if imported.CNAMETarget.ValueString() != "tun-1.example.net" {
		t.Errorf("cname_target = %q on an import", imported.CNAMETarget.ValueString())
	}

	configured := &tunnelResourceModel{
		ID:         types.StringValue("tun-1"),
		Name:       types.StringValue("lo-que-dice-la-config"),
		ExternalID: types.StringValue("mio"),
	}
	applyTunnelRead(configured, &nubulus.Tunnel{
		ID: "tun-1", Name: "otra-cosa", ExternalID: "otro",
	})
	if configured.Name.ValueString() != "lo-que-dice-la-config" {
		t.Errorf("a read overwrote the configured name: %q", configured.Name.ValueString())
	}
	if configured.ExternalID.ValueString() != "mio" {
		t.Errorf("a read overwrote the configured external_id: %q", configured.ExternalID.ValueString())
	}
}

// adopt_existing MUST default to false.
//
// The whole safety argument rests on it: taking over an existing tunnel issues
// a new credential, and that stops whatever is running on the old one. If the
// default ever flips, an apply that today refuses with an explanation would
// instead break a live tunnel, and nothing else in the code would look wrong.
func TestAdoptExistingDefaultsToRefusing(t *testing.T) {
	attr, ok := tunnelSchema(t).Attributes["adopt_existing"].(schema.BoolAttribute)
	if !ok {
		t.Fatal("adopt_existing is not a bool attribute")
	}
	if attr.Default == nil {
		t.Fatal("adopt_existing has no default; an unset value would be null, not false")
	}

	var resp defaults.BoolResponse
	attr.Default.DefaultBool(context.Background(), defaults.BoolRequest{}, &resp)
	if resp.PlanValue.ValueBool() {
		t.Error("adopt_existing defaults to true: an apply would take over a live tunnel and " +
			"rotate its credential without being asked")
	}
}

// Every attribute in the schema needs a field with a matching tfsdk tag, and
// every field needs an attribute. Getting it wrong compiles and passes every
// other test, then fails on the first real apply.
func TestTunnelSchemaAndModelAgree(t *testing.T) {
	inSchema := map[string]bool{}
	for name := range tunnelSchema(t).Attributes {
		inSchema[name] = true
	}

	inModel := map[string]bool{}
	modelType := reflect.TypeOf(tunnelResourceModel{})
	for i := range modelType.NumField() {
		tag := modelType.Field(i).Tag.Get("tfsdk")
		if tag == "" {
			t.Errorf("field %s has no tfsdk tag", modelType.Field(i).Name)
			continue
		}
		inModel[tag] = true
	}

	for name := range inSchema {
		if !inModel[name] {
			t.Errorf("attribute %q has no field in the model", name)
		}
	}
	for name := range inModel {
		if !inSchema[name] {
			t.Errorf("model field %q is not an attribute of the schema", name)
		}
	}
}

// The two attributes a user can set both replace the tunnel, because the API
// has no way to edit one. If either ever stops requiring replacement, a plan
// would promise a change that the apply cannot make.
func TestTheSettableAttributesRequireReplacement(t *testing.T) {
	s := tunnelSchema(t)
	for _, name := range []string{"name", "external_id"} {
		attr, ok := s.Attributes[name].(schema.StringAttribute)
		if !ok {
			t.Fatalf("%s is not a string attribute", name)
		}
		if len(attr.PlanModifiers) == 0 {
			t.Errorf("%s has no plan modifiers, so changing it would plan an update the API "+
				"cannot perform", name)
		}
	}
}
