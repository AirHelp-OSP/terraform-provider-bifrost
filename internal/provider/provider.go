// Package provider implements the Bifrost Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	bifrostclient "github.com/airhelp-osp/terraform-provider-bifrost/internal/client"
	"github.com/airhelp-osp/terraform-provider-bifrost/internal/resources"
)

// Ensure BifrostProvider satisfies the provider.Provider interface.
var _ provider.Provider = &BifrostProvider{}
var _ provider.ProviderWithFunctions = &BifrostProvider{}

// BifrostProvider is the Terraform provider implementation.
type BifrostProvider struct {
	version string
}

// BifrostProviderModel maps to the provider schema attributes.
type BifrostProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
}

// New returns a provider factory function.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &BifrostProvider{version: version}
	}
}

func (p *BifrostProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "bifrost"
	resp.Version = p.version
}

func (p *BifrostProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage [Bifrost](https://github.com/maximhq/bifrost) AI/LLM gateway " +
			"resources — provider configurations, virtual keys, and governance entities.",
		Description: "Interact with Bifrost AI/LLM gateway resources.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "Bifrost API base URL (e.g. `http://bifrost.internal:8080`). " +
					"May also be set via the `BIFROST_ENDPOINT` environment variable.",
				Description: "Bifrost API base URL. May also be set via BIFROST_ENDPOINT environment variable.",
				Required:    true,
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "Basic-auth username for the Bifrost admin API. " +
					"May also be set via the `BIFROST_USERNAME` environment variable.",
				Description: "Basic auth username. May also be set via BIFROST_USERNAME environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "Basic-auth password for the Bifrost admin API. " +
					"May also be set via the `BIFROST_PASSWORD` environment variable.",
				Description: "Basic auth password. May also be set via BIFROST_PASSWORD environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
		},
	}
}

func (p *BifrostProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config BifrostProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Reject unknown values — the client can't be constructed until configuration
	// is resolved, which means we can't defer work to apply-time.
	if config.Endpoint.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Unknown Bifrost endpoint",
			"The provider cannot be configured with an unknown endpoint. Set a static value or use BIFROST_ENDPOINT.",
		)
	}
	if config.Username.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("username"),
			"Unknown Bifrost username",
			"The provider cannot be configured with an unknown username. Set a static value or use BIFROST_USERNAME.",
		)
	}
	if config.Password.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("password"),
			"Unknown Bifrost password",
			"The provider cannot be configured with an unknown password. Set a static value or use BIFROST_PASSWORD.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := config.Endpoint.ValueString()
	if endpoint == "" {
		endpoint = os.Getenv("BIFROST_ENDPOINT")
	}
	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Missing Bifrost endpoint",
			"Set the endpoint attribute or BIFROST_ENDPOINT environment variable.",
		)
		return
	}

	username := config.Username.ValueString()
	if username == "" {
		username = os.Getenv("BIFROST_USERNAME")
	}

	password := config.Password.ValueString()
	if password == "" {
		password = os.Getenv("BIFROST_PASSWORD")
	}

	client := bifrostclient.New(endpoint, username, password)
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *BifrostProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		resources.NewProviderResource,
		resources.NewVirtualKeyResource,
	}
}

func (p *BifrostProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

func (p *BifrostProvider) Functions(_ context.Context) []func() function.Function {
	return nil
}
