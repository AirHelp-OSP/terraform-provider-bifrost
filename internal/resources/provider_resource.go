// Package resources implements Terraform resource types for Bifrost.
package resources

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/maximhq/bifrost/core/schemas"

	bifrostclient "github.com/airhelp-osp/terraform-provider-bifrost/internal/client"
)

var _ resource.Resource = &ProviderResource{}
var _ resource.ResourceWithImportState = &ProviderResource{}
var _ resource.ResourceWithConfigValidators = &ProviderResource{}

// NewProviderResource returns a new ProviderResource.
func NewProviderResource() resource.Resource {
	return &ProviderResource{}
}

// ProviderResource manages bifrost_provider resources.
//
// As of Bifrost v1.5.0 this resource manages only the provider's configuration
// (network, proxy, concurrency, custom-provider settings). API keys are managed
// via the separate bifrost_provider_key resource backed by the dedicated
// /api/providers/{provider}/keys endpoints.
type ProviderResource struct {
	client *bifrostclient.BifrostClient
}

// ── Model types ───────────────────────────────────────────────────────────────

type ProviderResourceModel struct {
	ID                       types.String                `tfsdk:"id"`
	ProviderName             types.String                `tfsdk:"provider_name"`
	NetworkConfig            *NetworkConfigModel         `tfsdk:"network_config"`
	ProxyConfig              *ProxyConfigModel           `tfsdk:"proxy_config"`
	ConcurrencyAndBufferSize *ConcurrencyBufferSizeModel `tfsdk:"concurrency_and_buffer_size"`
	SendBackRawRequest       types.Bool                  `tfsdk:"send_back_raw_request"`
	SendBackRawResponse      types.Bool                  `tfsdk:"send_back_raw_response"`
	CustomProviderConfig     *CustomProviderConfigModel  `tfsdk:"custom_provider_config"`
	ProviderStatus           types.String                `tfsdk:"provider_status"`
}

type NetworkConfigModel struct {
	BaseURL                        types.String `tfsdk:"base_url"`
	ExtraHeaders                   types.Map    `tfsdk:"extra_headers"`
	DefaultRequestTimeoutInSeconds types.Int64  `tfsdk:"default_request_timeout_in_seconds"`
	MaxRetries                     types.Int64  `tfsdk:"max_retries"`
	RetryBackoffInitialMs          types.Int64  `tfsdk:"retry_backoff_initial_ms"`
	RetryBackoffMaxMs              types.Int64  `tfsdk:"retry_backoff_max_ms"`
	InsecureSkipVerify             types.Bool   `tfsdk:"insecure_skip_verify"`
	CACertPEM                      types.String `tfsdk:"ca_cert_pem"`
}

type ProxyConfigModel struct {
	Type      types.String `tfsdk:"type"`
	URL       types.String `tfsdk:"url"`
	Username  types.String `tfsdk:"username"`
	Password  types.String `tfsdk:"password"`
	CACertPEM types.String `tfsdk:"ca_cert_pem"`
}

type ConcurrencyBufferSizeModel struct {
	Concurrency types.Int64 `tfsdk:"concurrency"`
	BufferSize  types.Int64 `tfsdk:"buffer_size"`
}

type CustomProviderConfigModel struct {
	BaseProviderType types.String `tfsdk:"base_provider_type"`
	IsKeyLess        types.Bool   `tfsdk:"is_key_less"`
}

// ── Schema ────────────────────────────────────────────────────────────────────

func (r *ProviderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_provider"
}

func (r *ProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a [Bifrost AI provider configuration](https://github.com/maximhq/bifrost) — " +
			"network settings, proxy, concurrency, and (optionally) a custom-provider base type. " +
			"Manage API keys for this provider with the separate `bifrost_provider_key` resource.",
		Description: "Manages a Bifrost AI provider configuration. Use bifrost_provider_key for keys.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Provider identifier (mirrors `provider_name`).",
				Description:         "Provider identifier (mirrors provider_name).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"provider_name": schema.StringAttribute{
				MarkdownDescription: "The provider identifier (e.g. `bedrock`, `openai`, `anthropic`). " +
					"Acts as the resource ID. Forces replacement when changed.",
				Description: "The provider identifier (e.g. 'bedrock', 'openai'). Acts as the resource ID.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"provider_status": schema.StringAttribute{
				MarkdownDescription: "Health/initialization status reported by Bifrost — typically `active` or `error`.",
				Description:         "Health/initialization status reported by Bifrost ('active', 'error').",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"send_back_raw_request": schema.BoolAttribute{
				MarkdownDescription: "Include the raw provider request in `BifrostResponse` for debugging. Defaults to `false`.",
				Description:         "Include the raw provider request in BifrostResponse.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"send_back_raw_response": schema.BoolAttribute{
				MarkdownDescription: "Include the raw provider response in `BifrostResponse` for debugging. Defaults to `false`.",
				Description:         "Include the raw provider response in BifrostResponse.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"network_config": schema.SingleNestedAttribute{
				MarkdownDescription: "Network configuration for upstream provider connections.",
				Description:         "Network configuration for provider connections.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"base_url": schema.StringAttribute{
						MarkdownDescription: "Override the provider base URL (e.g. for proxies or self-hosted endpoints).",
						Description:         "Override the provider base URL.",
						Optional:            true,
					},
					"extra_headers": schema.MapAttribute{
						MarkdownDescription: "Additional HTTP headers to include in upstream requests.",
						Description:         "Additional HTTP headers to include in requests.",
						Optional:            true,
						ElementType:         types.StringType,
					},
					"default_request_timeout_in_seconds": schema.Int64Attribute{
						MarkdownDescription: "Request timeout in seconds. Defaults to `30`.",
						Description:         "Request timeout in seconds.",
						Optional:            true,
						Computed:            true,
						Default:             int64default.StaticInt64(30),
					},
					"max_retries": schema.Int64Attribute{
						MarkdownDescription: "Maximum retry attempts on upstream failure. Defaults to `0`.",
						Description:         "Maximum retry attempts.",
						Optional:            true,
						Computed:            true,
						Default:             int64default.StaticInt64(0),
					},
					"retry_backoff_initial_ms": schema.Int64Attribute{
						MarkdownDescription: "Initial retry backoff in milliseconds. Defaults to `500`.",
						Description:         "Initial retry backoff in milliseconds.",
						Optional:            true,
						Computed:            true,
						Default:             int64default.StaticInt64(500),
					},
					"retry_backoff_max_ms": schema.Int64Attribute{
						MarkdownDescription: "Maximum retry backoff in milliseconds. Defaults to `5000`.",
						Description:         "Maximum retry backoff in milliseconds.",
						Optional:            true,
						Computed:            true,
						Default:             int64default.StaticInt64(5000),
					},
					"insecure_skip_verify": schema.BoolAttribute{
						MarkdownDescription: "Disable TLS certificate verification. Defaults to `false`. **Use with caution.**",
						Description:         "Disable TLS certificate verification.",
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
					},
					"ca_cert_pem": schema.StringAttribute{
						MarkdownDescription: "PEM-encoded CA certificate to trust. Bifrost redacts this on `GET`; the prior value is preserved on Read. " +
							"Supports `env.VAR_NAME` references (Bifrost v1.5.0+).",
						Description: "PEM-encoded CA certificate to trust.",
						Optional:    true,
						Computed:    true,
						Sensitive:   true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseStateForUnknown(),
						},
					},
				},
			},
			"proxy_config": schema.SingleNestedAttribute{
				MarkdownDescription: "Outbound proxy configuration for upstream provider connections. " +
					"`url`, `username`, `password`, and `ca_cert_pem` accept `env.VAR_NAME` references (Bifrost v1.5.0+).",
				Description: "Proxy configuration.",
				Optional:    true,
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						MarkdownDescription: "Proxy type. One of `none`, `http`, `socks5`, `environment`.",
						Description:         "Proxy type: 'none', 'http', 'socks5', or 'environment'.",
						Optional:            true,
						Validators: []validator.String{
							stringvalidator.OneOf("none", "http", "socks5", "environment"),
						},
					},
					"url": schema.StringAttribute{
						MarkdownDescription: "Proxy server URL.",
						Description:         "Proxy server URL.",
						Optional:            true,
					},
					"username": schema.StringAttribute{
						MarkdownDescription: "Proxy authentication username.",
						Description:         "Proxy authentication username.",
						Optional:            true,
					},
					"password": schema.StringAttribute{
						MarkdownDescription: "Proxy authentication password.",
						Description:         "Proxy authentication password.",
						Optional:            true,
						Sensitive:           true,
					},
					"ca_cert_pem": schema.StringAttribute{
						MarkdownDescription: "PEM-encoded CA certificate to trust for TLS connections through the proxy.",
						Description:         "PEM-encoded CA certificate to trust for TLS connections through the proxy.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseStateForUnknown(),
						},
					},
				},
			},
			"concurrency_and_buffer_size": schema.SingleNestedAttribute{
				MarkdownDescription: "Concurrency and buffer settings for the upstream worker pool.",
				Description:         "Concurrency and buffer settings.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"concurrency": schema.Int64Attribute{
						MarkdownDescription: "Number of concurrent operations. Defaults to `1000`.",
						Description:         "Number of concurrent operations.",
						Optional:            true,
						Computed:            true,
						Default:             int64default.StaticInt64(1000),
					},
					"buffer_size": schema.Int64Attribute{
						MarkdownDescription: "Buffer size (must be ≥ `concurrency`). Defaults to `5000`.",
						Description:         "Buffer size (must be >= concurrency).",
						Optional:            true,
						Computed:            true,
						Default:             int64default.StaticInt64(5000),
					},
				},
			},
			"custom_provider_config": schema.SingleNestedAttribute{
				MarkdownDescription: "Custom provider configuration — base a non-standard provider on a standard one.",
				Description:         "Custom provider configuration (for non-standard providers).",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"base_provider_type": schema.StringAttribute{
						MarkdownDescription: "The standard provider type to base this custom provider on (e.g. `openai`).",
						Description:         "The standard provider type to base this custom provider on.",
						Required:            true,
					},
					"is_key_less": schema.BoolAttribute{
						MarkdownDescription: "Whether the custom provider operates without an API key.",
						Description:         "Whether the custom provider requires an API key.",
						Optional:            true,
					},
				},
			},
		},
	}
}

// ── CRUD ──────────────────────────────────────────────────────────────────────

// bufferSizeAtLeastConcurrencyValidator enforces the schema-documented invariant
// that buffer_size >= concurrency when the user supplies both values. When
// either is left unset, framework defaults satisfy the relationship.
type bufferSizeAtLeastConcurrencyValidator struct{}

func (bufferSizeAtLeastConcurrencyValidator) Description(_ context.Context) string {
	return "concurrency_and_buffer_size.buffer_size must be >= concurrency_and_buffer_size.concurrency"
}

func (v bufferSizeAtLeastConcurrencyValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (bufferSizeAtLeastConcurrencyValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model ProviderResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cb := model.ConcurrencyAndBufferSize
	if cb == nil {
		return
	}
	if cb.Concurrency.IsNull() || cb.Concurrency.IsUnknown() {
		return
	}
	if cb.BufferSize.IsNull() || cb.BufferSize.IsUnknown() {
		return
	}
	if cb.BufferSize.ValueInt64() < cb.Concurrency.ValueInt64() {
		resp.Diagnostics.AddAttributeError(
			path.Root("concurrency_and_buffer_size").AtName("buffer_size"),
			"Invalid buffer_size",
			fmt.Sprintf("buffer_size (%d) must be >= concurrency (%d).",
				cb.BufferSize.ValueInt64(), cb.Concurrency.ValueInt64()),
		)
	}
}

func (r *ProviderResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		bufferSizeAtLeastConcurrencyValidator{},
	}
}

func (r *ProviderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*bifrostclient.BifrostClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type",
			fmt.Sprintf("Expected *client.BifrostClient, got %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *ProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "creating Bifrost provider", map[string]any{"provider_name": plan.ProviderName.ValueString()})

	createReq, diags := modelToCreateRequest(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResp, err := r.client.CreateProvider(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Error creating provider", err.Error())
		return
	}

	newState, diags := responseToModel(apiResp, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "created Bifrost provider", map[string]any{"provider_name": newState.ProviderName.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *ProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "reading Bifrost provider", map[string]any{"provider_name": state.ProviderName.ValueString()})

	apiResp, err := r.client.GetProvider(ctx, state.ProviderName.ValueString())
	if err != nil {
		if bifrostclient.IsNotFound(err) {
			tflog.Debug(ctx, "Bifrost provider not found, removing from state",
				map[string]any{"provider_name": state.ProviderName.ValueString()})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading provider", err.Error())
		return
	}

	newState, diags := responseToModel(apiResp, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *ProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "updating Bifrost provider", map[string]any{"provider_name": plan.ProviderName.ValueString()})

	updateReq, diags := modelToUpdateRequest(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResp, err := r.client.UpdateProvider(ctx, plan.ProviderName.ValueString(), updateReq)
	if err != nil {
		resp.Diagnostics.AddError("Error updating provider", err.Error())
		return
	}

	newState, diags := responseToModel(apiResp, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *ProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "deleting Bifrost provider", map[string]any{"provider_name": state.ProviderName.ValueString()})

	err := r.client.DeleteProvider(ctx, state.ProviderName.ValueString())
	if err != nil && !bifrostclient.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting provider", err.Error())
		return
	}
}

func (r *ProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("provider_name"), req, resp)
}

// ── Conversion: model → API request ──────────────────────────────────────────

func modelToCreateRequest(ctx context.Context, m ProviderResourceModel) (bifrostclient.CreateProviderRequest, diag.Diagnostics) {
	var diags diag.Diagnostics
	req := bifrostclient.CreateProviderRequest{
		Provider: schemas.ModelProvider(m.ProviderName.ValueString()),
	}

	if m.NetworkConfig != nil {
		nc, d := modelToNetworkConfig(ctx, m.NetworkConfig)
		diags.Append(d...)
		if !diags.HasError() {
			req.NetworkConfig = &nc
		}
	}
	if m.ProxyConfig != nil {
		pc := modelToProxyConfig(m.ProxyConfig)
		req.ProxyConfig = &pc
	}
	if m.ConcurrencyAndBufferSize != nil {
		cb := modelToConcurrencyAndBufferSize(m.ConcurrencyAndBufferSize)
		req.ConcurrencyAndBufferSize = &cb
	}
	if !m.SendBackRawRequest.IsNull() && !m.SendBackRawRequest.IsUnknown() {
		v := m.SendBackRawRequest.ValueBool()
		req.SendBackRawRequest = &v
	}
	if !m.SendBackRawResponse.IsNull() && !m.SendBackRawResponse.IsUnknown() {
		v := m.SendBackRawResponse.ValueBool()
		req.SendBackRawResponse = &v
	}
	if m.CustomProviderConfig != nil {
		cpc := modelToCustomProviderConfig(m.CustomProviderConfig)
		req.CustomProviderConfig = &cpc
	}
	return req, diags
}

func modelToUpdateRequest(ctx context.Context, m ProviderResourceModel) (bifrostclient.UpdateProviderRequest, diag.Diagnostics) {
	var diags diag.Diagnostics
	req := bifrostclient.UpdateProviderRequest{}

	if m.NetworkConfig != nil {
		nc, d := modelToNetworkConfig(ctx, m.NetworkConfig)
		diags.Append(d...)
		if !diags.HasError() {
			req.NetworkConfig = &nc
		}
	}
	if m.ConcurrencyAndBufferSize != nil {
		cb := modelToConcurrencyAndBufferSize(m.ConcurrencyAndBufferSize)
		req.ConcurrencyAndBufferSize = &cb
	}
	if m.ProxyConfig != nil {
		pc := modelToProxyConfig(m.ProxyConfig)
		req.ProxyConfig = &pc
	}
	if !m.SendBackRawRequest.IsNull() && !m.SendBackRawRequest.IsUnknown() {
		v := m.SendBackRawRequest.ValueBool()
		req.SendBackRawRequest = &v
	}
	if !m.SendBackRawResponse.IsNull() && !m.SendBackRawResponse.IsUnknown() {
		v := m.SendBackRawResponse.ValueBool()
		req.SendBackRawResponse = &v
	}
	if m.CustomProviderConfig != nil {
		cpc := modelToCustomProviderConfig(m.CustomProviderConfig)
		req.CustomProviderConfig = &cpc
	}
	return req, diags
}

// ── Conversion: API response → model ─────────────────────────────────────────

// hasPriorState reports whether prior was produced by a previous Read/Create/Update.
// ProviderStatus is computed-only and unconditionally set by responseToModel, so it is
// IsNull() iff this state was created by ImportStatePassthroughID and has not yet been
// reconciled with the API. Distinguishing those cases lets the nested-block guards
// preserve "user did not configure this block" semantics on regular Read while still
// hydrating state from the API on the post-import Read.
func hasPriorState(prior *ProviderResourceModel) bool {
	return prior != nil && !prior.ProviderStatus.IsNull()
}

// responseToModel converts a ProviderResponse to TF state, preserving sensitive fields
// from prior when the API returns redacted values. Nested-block presence in state
// follows the user's original configuration: if `prior.<block>` was nil, state stays
// nil even when the API returns server-side defaults — except on the post-import Read,
// where prior is the empty passthrough shape and we hydrate from the API instead.
func responseToModel(apiResp *bifrostclient.ProviderResponse, prior *ProviderResourceModel) (*ProviderResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	m := &ProviderResourceModel{
		ID:                  types.StringValue(string(apiResp.Name)),
		ProviderName:        types.StringValue(string(apiResp.Name)),
		SendBackRawRequest:  types.BoolValue(apiResp.SendBackRawRequest),
		SendBackRawResponse: types.BoolValue(apiResp.SendBackRawResponse),
		ProviderStatus:      types.StringValue(apiResp.ProviderStatus),
	}

	// network_config: only populate state if the user configured the block.
	// On import, prior has no ProviderStatus yet, so hydrate from the API.
	if !hasPriorState(prior) || prior.NetworkConfig != nil {
		ncRes := networkConfigToModel(apiResp.NetworkConfig, prior)
		diags.Append(ncRes.diags...)
		m.NetworkConfig = ncRes.model
	}

	// proxy_config: API returns nil if unconfigured.
	if apiResp.ProxyConfig != nil {
		pc := apiResp.ProxyConfig
		pcModel := &ProxyConfigModel{
			Type: types.StringValue(string(pc.Type)),
		}
		var priorPC *ProxyConfigModel
		if prior != nil {
			priorPC = prior.ProxyConfig
		}
		pcModel.URL = envVarToString(pc.URL, priorStringOrNull(priorPC, func(p *ProxyConfigModel) types.String { return p.URL }))
		pcModel.Username = envVarToString(pc.Username, priorStringOrNull(priorPC, func(p *ProxyConfigModel) types.String { return p.Username }))
		pcModel.Password = envVarToString(pc.Password, priorStringOrNull(priorPC, func(p *ProxyConfigModel) types.String { return p.Password }))
		pcModel.CACertPEM = envVarToString(pc.CACertPEM, priorStringOrNull(priorPC, func(p *ProxyConfigModel) types.String { return p.CACertPEM }))
		m.ProxyConfig = pcModel
	}

	// concurrency_and_buffer_size: only populate state if the user configured the block.
	// On import, prior has no ProviderStatus yet, so hydrate from the API.
	if !hasPriorState(prior) || prior.ConcurrencyAndBufferSize != nil {
		cb := apiResp.ConcurrencyAndBufferSize
		m.ConcurrencyAndBufferSize = &ConcurrencyBufferSizeModel{
			Concurrency: types.Int64Value(int64(cb.Concurrency)),
			BufferSize:  types.Int64Value(int64(cb.BufferSize)),
		}
	}

	if apiResp.CustomProviderConfig != nil {
		m.CustomProviderConfig = &CustomProviderConfigModel{
			BaseProviderType: types.StringValue(string(apiResp.CustomProviderConfig.BaseProviderType)),
			IsKeyLess:        types.BoolValue(apiResp.CustomProviderConfig.IsKeyLess),
		}
	}

	return m, diags
}

// priorStringOrNull returns the named string from a prior model, or null if the
// prior model itself is nil. Used to feed envVarToString with a sensible
// "previous value" so redacted server responses don't blow away user state.
func priorStringOrNull(prior *ProxyConfigModel, get func(*ProxyConfigModel) types.String) types.String {
	if prior == nil {
		return types.StringNull()
	}
	return get(prior)
}

type ncModelResult struct {
	model *NetworkConfigModel
	diags diag.Diagnostics
}

func networkConfigToModel(nc schemas.NetworkConfig, prior *ProviderResourceModel) ncModelResult {
	var diags diag.Diagnostics
	m := &NetworkConfigModel{
		BaseURL:                        emptyStringAsNull(nc.BaseURL),
		DefaultRequestTimeoutInSeconds: types.Int64Value(int64(nc.DefaultRequestTimeoutInSeconds)),
		MaxRetries:                     types.Int64Value(int64(nc.MaxRetries)),
		RetryBackoffInitialMs:          types.Int64Value(nc.RetryBackoffInitial.Milliseconds()),
		RetryBackoffMaxMs:              types.Int64Value(nc.RetryBackoffMax.Milliseconds()),
		InsecureSkipVerify:             types.BoolValue(nc.InsecureSkipVerify),
	}

	var priorCA types.String
	if prior != nil && prior.NetworkConfig != nil {
		priorCA = prior.NetworkConfig.CACertPEM
	} else {
		priorCA = types.StringNull()
	}
	m.CACertPEM = envVarToString(nc.CACertPEM, priorCA)

	if len(nc.ExtraHeaders) > 0 {
		elems := make(map[string]attr.Value, len(nc.ExtraHeaders))
		for k, v := range nc.ExtraHeaders {
			elems[k] = types.StringValue(v)
		}
		headersMap, d := types.MapValue(types.StringType, elems)
		diags.Append(d...)
		m.ExtraHeaders = headersMap
	} else {
		m.ExtraHeaders = types.MapNull(types.StringType)
	}

	return ncModelResult{model: m, diags: diags}
}

func modelToNetworkConfig(ctx context.Context, m *NetworkConfigModel) (schemas.NetworkConfig, diag.Diagnostics) {
	var diags diag.Diagnostics
	nc := schemas.NetworkConfig{
		BaseURL:                        m.BaseURL.ValueString(),
		DefaultRequestTimeoutInSeconds: int(m.DefaultRequestTimeoutInSeconds.ValueInt64()),
		MaxRetries:                     int(m.MaxRetries.ValueInt64()),
		InsecureSkipVerify:             m.InsecureSkipVerify.ValueBool(),
		CACertPEM:                      stringToEnvVar(m.CACertPEM),
	}
	if !m.RetryBackoffInitialMs.IsNull() && !m.RetryBackoffInitialMs.IsUnknown() {
		nc.RetryBackoffInitial = time.Duration(m.RetryBackoffInitialMs.ValueInt64()) * time.Millisecond
	}
	if !m.RetryBackoffMaxMs.IsNull() && !m.RetryBackoffMaxMs.IsUnknown() {
		nc.RetryBackoffMax = time.Duration(m.RetryBackoffMaxMs.ValueInt64()) * time.Millisecond
	}
	if !m.ExtraHeaders.IsNull() && !m.ExtraHeaders.IsUnknown() {
		hdrs := make(map[string]string)
		d := m.ExtraHeaders.ElementsAs(ctx, &hdrs, false)
		diags.Append(d...)
		nc.ExtraHeaders = hdrs
	}
	return nc, diags
}

func modelToProxyConfig(m *ProxyConfigModel) schemas.ProxyConfig {
	return schemas.ProxyConfig{
		Type:      schemas.ProxyType(m.Type.ValueString()),
		URL:       stringToEnvVar(m.URL),
		Username:  stringToEnvVar(m.Username),
		Password:  stringToEnvVar(m.Password),
		CACertPEM: stringToEnvVar(m.CACertPEM),
	}
}

func modelToConcurrencyAndBufferSize(m *ConcurrencyBufferSizeModel) schemas.ConcurrencyAndBufferSize {
	return schemas.ConcurrencyAndBufferSize{
		Concurrency: int(m.Concurrency.ValueInt64()),
		BufferSize:  int(m.BufferSize.ValueInt64()),
	}
}

func modelToCustomProviderConfig(m *CustomProviderConfigModel) schemas.CustomProviderConfig {
	return schemas.CustomProviderConfig{
		BaseProviderType: schemas.ModelProvider(m.BaseProviderType.ValueString()),
		IsKeyLess:        m.IsKeyLess.ValueBool(),
	}
}
