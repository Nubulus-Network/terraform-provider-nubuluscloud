//go:build generate

package tools

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)

// Format the Terraform snippets that end up in the documentation.
//go:generate terraform fmt -recursive ../examples/

// Generate docs/ from the schemas and the examples. The provider name decides
// the file names and the `nubuluscloud_` prefix the pages are indexed under.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-dir .. -provider-name nubuluscloud
