package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/nubulus-network/terraform-provider-nubuluscloud/internal/nubulus"
)

// configureClient reads the client the provider put in ProviderData.
//
// The nil check is not defensive noise: the framework calls Configure on every
// resource with a nil ProviderData during validation, before the provider
// itself has been configured, and a resource that dereferenced it there would
// panic on `terraform validate`.
func configureClient(providerData any, diags *diag.Diagnostics) *nubulus.Client {
	if providerData == nil {
		return nil
	}
	client, ok := providerData.(*nubulus.Client)
	if !ok {
		diags.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *nubulus.Client, got %T. This is a bug in the provider.", providerData),
		)
		return nil
	}
	return client
}

// addAPIError appends the diagnostic for a failed API call, with the
// translation that turns three misleading failures into actionable ones.
func addAPIError(diags *diag.Diagnostics, action string, err error) {
	summary, detail := nubulus.Explain(action, err)
	diags.AddError(summary, detail)
}

// rfc3339 is the one spelling of a timestamp the provider emits.
const rfc3339 = time.RFC3339

// stringList turns a slice into a Terraform list, empty rather than null when
// there is nothing in it: a zone with no nameservers read back is a zone whose
// list is empty, and null would read as "not known".
func stringList(values []string) types.List {
	elements := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elements = append(elements, types.StringValue(v))
	}
	// The error return is impossible here (every element is a StringValue and
	// the element type is StringType), so it is dropped rather than plumbed
	// through every caller.
	list, _ := types.ListValue(types.StringType, elements)
	return list
}

// stringSlice reads a Terraform set of strings into a Go slice.
func stringSlice(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	var values []string
	diags := set.ElementsAs(ctx, &values, false)
	return values, diags
}

// stringSet turns a slice into a Terraform set.
func stringSet(values []string) (types.Set, diag.Diagnostics) {
	elements := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elements = append(elements, types.StringValue(v))
	}
	return types.SetValue(types.StringType, elements)
}

// tflogWarn records a failure the provider chose to carry on from. There are
// only a couple, and each one says at its call site why continuing is right.
func tflogWarn(ctx context.Context, message string, err error) {
	tflog.Warn(ctx, message, map[string]any{"error": err.Error()})
}

// timeValue renders an optional timestamp as RFC 3339, or null.
//
// Times are strings in the schema rather than a custom type: they are all
// computed, nothing interpolates them into an API call, and RFC 3339 is what
// somebody reading `terraform show` expects to see.
func timeValue(t *time.Time) types.String {
	if t == nil {
		return types.StringNull()
	}
	return types.StringValue(t.Format(time.RFC3339))
}

// stringValue renders a possibly-empty string as null rather than "".
//
// The distinction matters for the fields that are absent instead of empty
// (primary_error on a healthy zone, verification on one that never needed it),
// because "" in state reads as "the API said empty" when what happened is that
// the API said nothing.
func stringValue(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
