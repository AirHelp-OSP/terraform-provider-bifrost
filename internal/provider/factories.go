package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// ProtoV6ProviderFactories returns provider factories for use with
// terraform-plugin-testing's resource.Test and resource.UnitTest.
//
// Lives in a non-test file so it can be imported by tests in sibling
// packages (e.g. internal/resources). Has zero runtime cost outside of
// tests because nothing in production code calls it.
//
// Tests that need network access should gate themselves on TF_ACC and
// stand up an httptest.Server to back BIFROST_ENDPOINT before calling
// these factories.
func ProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"bifrost": providerserver.NewProtocol6WithError(New("test")()),
	}
}
