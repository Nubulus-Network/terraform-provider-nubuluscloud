package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

// Once a zone is verified the API stops reporting its challenge, and the
// attribute must NOT fall back to null.
//
// The flow this provider documents publishes the challenge record with
// `values = ["\"${...verification_txt_value}\""]`, and Terraform evaluates that
// expression on every later plan, including the destroy. Blanking the value
// turns every command after a successful verification into "Invalid template
// interpolation value", with no way out but editing the configuration that had
// just worked. It cost a real destroy to find.
func TestTheChallengeSurvivesVerification(t *testing.T) {
	state := &dnsZoneResourceModel{
		VerificationTXTHost:  types.StringValue("_nubulus-challenge.tf.example.com"),
		VerificationTXTValue: types.StringValue("un-token"),
	}

	// What the API answers for a zone that is now active: no verification block.
	applyZoneDetail(state, &nubulus.ZoneDetail{
		Zone: &nubulus.Zone{ID: "zone_1", Name: "tf.example.com", Status: "active"},
	})

	if state.VerificationTXTValue.IsNull() {
		t.Error("the challenge value was blanked; every later plan would fail to interpolate it")
	}
	if state.VerificationTXTHost.ValueString() != "_nubulus-challenge.tf.example.com" {
		t.Errorf("host = %q", state.VerificationTXTHost.ValueString())
	}
	if state.VerificationRequired.ValueBool() {
		t.Error("a verified zone requires nothing")
	}
}

// A zone that never had a challenge (one already assigned to the account)
// keeps a null, because inventing a value would be worse than reporting none.
func TestAZoneThatNeverNeededAChallengeReportsNone(t *testing.T) {
	state := &dnsZoneResourceModel{
		VerificationTXTHost:  types.StringUnknown(),
		VerificationTXTValue: types.StringUnknown(),
	}

	applyZoneDetail(state, &nubulus.ZoneDetail{
		Zone: &nubulus.Zone{ID: "zone_1", Name: "example.com", Status: "active", Source: "neodigit"},
	})

	if !state.VerificationTXTValue.IsNull() {
		t.Errorf("value = %q, want null", state.VerificationTXTValue.ValueString())
	}
}
