package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

func tunnelRouteSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	NewTunnelRouteResource().(*tunnelRouteResource).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("building the schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// Creating a route cannot set `enabled` at all, and sends `priority` in a way
// that reads zero as "unset" and turns it into 100. Both are settable only
// through the update.
//
// So a create has to notice when it landed somewhere other than the plan and
// correct it. Without that, `enabled = false` produces "Provider produced
// inconsistent result after apply", and `priority = 0` produces a diff that
// comes back on every single plan for ever.
func TestACreateIsCorrectedWhenTheAPICannotHonourThePlan(t *testing.T) {
	for _, tc := range []struct {
		name         string
		plan         tunnelRouteResourceModel
		created      nubulus.Route
		wantNeeded   bool
		wantEnabled  *bool
		wantPriority *int
	}{
		{
			name:       "the ordinary route needs no second request",
			plan:       tunnelRouteResourceModel{Enabled: types.BoolValue(true), Priority: types.Int64Value(100)},
			created:    nubulus.Route{Enabled: true, Priority: 100},
			wantNeeded: false,
		},
		{
			name: "disabled: the API always creates a route enabled",
			plan: tunnelRouteResourceModel{Enabled: types.BoolValue(false), Priority: types.Int64Value(100)},
			// What the API answers: enabled, whatever was asked.
			created:     nubulus.Route{Enabled: true, Priority: 100},
			wantNeeded:  true,
			wantEnabled: boolPtr(false),
		},
		{
			name: "priority zero: the API reads it as unset and stores 100",
			plan: tunnelRouteResourceModel{Enabled: types.BoolValue(true), Priority: types.Int64Value(0)},
			// What the API answers: 100, not the 0 that was sent.
			created:      nubulus.Route{Enabled: true, Priority: 100},
			wantNeeded:   true,
			wantPriority: intPtr(0),
		},
		{
			name:         "both at once",
			plan:         tunnelRouteResourceModel{Enabled: types.BoolValue(false), Priority: types.Int64Value(0)},
			created:      nubulus.Route{Enabled: true, Priority: 100},
			wantNeeded:   true,
			wantEnabled:  boolPtr(false),
			wantPriority: intPtr(0),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fix, needed := routeCorrection(&tc.plan, &tc.created)

			if needed != tc.wantNeeded {
				t.Fatalf("needed = %v, want %v", needed, tc.wantNeeded)
			}
			if !needed {
				return
			}

			switch {
			case tc.wantEnabled == nil && fix.Enabled != nil:
				t.Errorf("enabled was sent when it did not need to be: %v", *fix.Enabled)
			case tc.wantEnabled != nil && (fix.Enabled == nil || *fix.Enabled != *tc.wantEnabled):
				t.Errorf("enabled = %v, want %v", fix.Enabled, *tc.wantEnabled)
			}
			switch {
			case tc.wantPriority == nil && fix.Priority != nil:
				t.Errorf("priority was sent when it did not need to be: %v", *fix.Priority)
			case tc.wantPriority != nil && (fix.Priority == nil || *fix.Priority != *tc.wantPriority):
				t.Errorf("priority = %v, want %v", fix.Priority, *tc.wantPriority)
			}
		})
	}
}

// The three fields the API cannot edit have to replace the route. If any of
// them ever became updatable, a plan would promise to change a hostname and the
// apply would change nothing — the worst thing a provider can do, because it
// reports success.
func TestTheUneditableFieldsRequireReplacement(t *testing.T) {
	s := tunnelRouteSchema(t)
	for _, name := range []string{"tunnel_id", "type", "hostname", "path_prefix"} {
		attr, ok := s.Attributes[name].(schema.StringAttribute)
		if !ok {
			t.Fatalf("%s is not a string attribute", name)
		}
		if len(attr.PlanModifiers) == 0 {
			t.Errorf("%s can be planned as an in-place update, and the API has no way to do it", name)
		}
	}
}

// A path route with no prefix, or with "/", matches everything — which is what
// a host route is for. Catching it at plan time turns a 400 halfway through an
// apply into a message before anything is sent.
func TestAPathRouteNeedsARealPrefix(t *testing.T) {
	for _, tc := range []struct {
		name      string
		routeType string
		prefix    types.String
		wantError bool
	}{
		{"path with no prefix", "path", types.StringNull(), true},
		{"path with the root prefix", "path", types.StringValue("/"), true},
		{"path with a prefix missing its slash", "path", types.StringValue("api"), true},
		{"path with a real prefix", "path", types.StringValue("/api"), false},
		{"host with no prefix", "host", types.StringNull(), false},
		{"host with the root prefix", "host", types.StringValue("/"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateRouteConfig(t, tc.routeType, tc.prefix)
			if diags.HasError() != tc.wantError {
				t.Errorf("rejected = %v, want %v (%v)", diags.HasError(), tc.wantError, diags)
			}
		})
	}
}

// validateRouteConfig runs the resource's real ValidateConfig against a
// configuration, rather than a copy of its logic — a test that mirrors the
// implementation agrees with it by construction and catches nothing.
func validateRouteConfig(t *testing.T, routeType string, prefix types.String) diag.Diagnostics {
	t.Helper()
	ctx := context.Background()
	s := tunnelRouteSchema(t)

	prefixValue := tftypes.NewValue(tftypes.String, nil)
	if !prefix.IsNull() {
		prefixValue = tftypes.NewValue(tftypes.String, prefix.ValueString())
	}

	// Everything the rule does not look at is null; the shape still has to be
	// complete, because the config object is typed.
	raw := tftypes.NewValue(s.Type().TerraformType(ctx), map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, nil),
		"tunnel_id":       tftypes.NewValue(tftypes.String, "tun-1"),
		"type":            tftypes.NewValue(tftypes.String, routeType),
		"hostname":        tftypes.NewValue(tftypes.String, "app.example.com"),
		"path_prefix":     prefixValue,
		"upstream_host":   tftypes.NewValue(tftypes.String, "127.0.0.1"),
		"upstream_port":   tftypes.NewValue(tftypes.Number, 8080),
		"upstream_scheme": tftypes.NewValue(tftypes.String, nil),
		"strip_prefix":    tftypes.NewValue(tftypes.Bool, nil),
		"enabled":         tftypes.NewValue(tftypes.Bool, nil),
		"priority":        tftypes.NewValue(tftypes.Number, nil),
	})

	var resp resource.ValidateConfigResponse
	NewTunnelRouteResource().(*tunnelRouteResource).ValidateConfig(ctx,
		resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: s, Raw: raw}}, &resp)
	return resp.Diagnostics
}

func TestTunnelRouteSchemaAndModelAgree(t *testing.T) {
	inSchema := map[string]bool{}
	for name := range tunnelRouteSchema(t).Attributes {
		inSchema[name] = true
	}

	inModel := map[string]bool{}
	modelType := reflect.TypeOf(tunnelRouteResourceModel{})
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

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }
