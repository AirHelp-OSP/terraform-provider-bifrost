//go:build tools

// Package tools pins build-time dependencies (e.g. tfplugindocs) so that
// `go mod tidy` keeps them in go.sum without leaking into runtime imports.
// See https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
package tools

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)
