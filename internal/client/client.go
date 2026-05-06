// Package client provides an HTTP client for the Bifrost REST API.
package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
)

// maxResponseBytes caps the response body we buffer from Bifrost to avoid OOM
// on a misbehaving server.
const maxResponseBytes = 10 * 1024 * 1024

// defaultHTTPTimeout is applied when the caller does not supply a custom client.
const defaultHTTPTimeout = 30 * time.Second

// BifrostClient is an authenticated HTTP client for the Bifrost API.
type BifrostClient struct {
	BaseURL    string
	Username   string
	Password   string
	HTTPClient *http.Client
}

// New creates a new BifrostClient with a sensible default HTTP timeout.
func New(baseURL, username, password string) *BifrostClient {
	return &BifrostClient{
		BaseURL:    baseURL,
		Username:   username,
		Password:   password,
		HTTPClient: &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// APIError represents an error response from the Bifrost API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bifrost API error %d: %s", e.StatusCode, e.Body)
}

// doRequest performs an authenticated HTTP request and decodes the JSON response into result.
// result may be nil if no response body is expected.
func (c *BifrostClient) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	if c.Username != "" || c.Password != "" {
		creds := base64.StdEncoding.EncodeToString([]byte(c.Username + ":" + c.Password))
		req.Header.Set("Authorization", "Basic "+creds)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

// IsNotFound returns true if err is a 404 API error.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// IsConflict returns true if err is a 409 API error.
func IsConflict(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusConflict
	}
	return false
}

// ─── Provider API types ───────────────────────────────────────────────────────

// CreateProviderRequest is the payload for POST /api/providers.
type CreateProviderRequest struct {
	Provider                 schemas.ModelProvider             `json:"provider"`
	Keys                     []schemas.Key                     `json:"keys,omitempty"`
	NetworkConfig            *schemas.NetworkConfig            `json:"network_config,omitempty"`
	ConcurrencyAndBufferSize *schemas.ConcurrencyAndBufferSize `json:"concurrency_and_buffer_size,omitempty"`
	ProxyConfig              *schemas.ProxyConfig              `json:"proxy_config,omitempty"`
	SendBackRawRequest       *bool                             `json:"send_back_raw_request,omitempty"`
	SendBackRawResponse      *bool                             `json:"send_back_raw_response,omitempty"`
	CustomProviderConfig     *schemas.CustomProviderConfig     `json:"custom_provider_config,omitempty"`
	OpenAIConfig             *schemas.OpenAIConfig             `json:"openai_config,omitempty"`
}

// UpdateProviderRequest is the payload for PUT /api/providers/{name} (full replace).
// Omitted fields are left unchanged by Bifrost; this matches how the resource's
// Optional-only nested blocks behave when the user does not configure them.
type UpdateProviderRequest struct {
	Keys                     []schemas.Key                     `json:"keys"`
	NetworkConfig            *schemas.NetworkConfig            `json:"network_config,omitempty"`
	ConcurrencyAndBufferSize *schemas.ConcurrencyAndBufferSize `json:"concurrency_and_buffer_size,omitempty"`
	ProxyConfig              *schemas.ProxyConfig              `json:"proxy_config,omitempty"`
	SendBackRawRequest       *bool                             `json:"send_back_raw_request,omitempty"`
	SendBackRawResponse      *bool                             `json:"send_back_raw_response,omitempty"`
	CustomProviderConfig     *schemas.CustomProviderConfig     `json:"custom_provider_config,omitempty"`
	OpenAIConfig             *schemas.OpenAIConfig             `json:"openai_config,omitempty"`
}

// ProviderResponse mirrors handlers.ProviderResponse.
type ProviderResponse struct {
	Name                     schemas.ModelProvider            `json:"name"`
	Keys                     []schemas.Key                    `json:"keys"`
	NetworkConfig            schemas.NetworkConfig            `json:"network_config"`
	ConcurrencyAndBufferSize schemas.ConcurrencyAndBufferSize `json:"concurrency_and_buffer_size"`
	ProxyConfig              *schemas.ProxyConfig             `json:"proxy_config"`
	SendBackRawRequest       bool                             `json:"send_back_raw_request"`
	SendBackRawResponse      bool                             `json:"send_back_raw_response"`
	CustomProviderConfig     *schemas.CustomProviderConfig    `json:"custom_provider_config,omitempty"`
	OpenAIConfig             *schemas.OpenAIConfig            `json:"openai_config,omitempty"`
	ProviderStatus           string                           `json:"provider_status"`
}

// CreateProvider calls POST /api/providers.
func (c *BifrostClient) CreateProvider(ctx context.Context, req CreateProviderRequest) (*ProviderResponse, error) {
	var resp ProviderResponse
	if err := c.doRequest(ctx, http.MethodPost, "/api/providers", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetProvider calls GET /api/providers/{name}.
func (c *BifrostClient) GetProvider(ctx context.Context, name string) (*ProviderResponse, error) {
	var resp ProviderResponse
	if err := c.doRequest(ctx, http.MethodGet, "/api/providers/"+url.PathEscape(name), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateProvider calls PUT /api/providers/{name}.
func (c *BifrostClient) UpdateProvider(ctx context.Context, name string, req UpdateProviderRequest) (*ProviderResponse, error) {
	var resp ProviderResponse
	if err := c.doRequest(ctx, http.MethodPut, "/api/providers/"+url.PathEscape(name), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteProvider calls DELETE /api/providers/{name}.
func (c *BifrostClient) DeleteProvider(ctx context.Context, name string) error {
	return c.doRequest(ctx, http.MethodDelete, "/api/providers/"+url.PathEscape(name), nil, nil)
}

// ─── Virtual Key API types ────────────────────────────────────────────────────

// VKBudget is the budget shape used in both create requests and API responses.
type VKBudget struct {
	MaxLimit        float64 `json:"max_limit"`
	ResetDuration   string  `json:"reset_duration"`
	CalendarAligned bool    `json:"calendar_aligned,omitempty"`
}

// VKUpdateBudget is the budget shape used in update requests (all pointer fields).
type VKUpdateBudget struct {
	MaxLimit        *float64 `json:"max_limit,omitempty"`
	ResetDuration   *string  `json:"reset_duration,omitempty"`
	CalendarAligned *bool    `json:"calendar_aligned,omitempty"`
}

// VKRateLimit is the rate limit shape used in create requests and API responses.
type VKRateLimit struct {
	TokenMaxLimit        *int64  `json:"token_max_limit,omitempty"`
	TokenResetDuration   *string `json:"token_reset_duration,omitempty"`
	RequestMaxLimit      *int64  `json:"request_max_limit,omitempty"`
	RequestResetDuration *string `json:"request_reset_duration,omitempty"`
}

// VKProviderConfigCreate is a provider config entry in a create or update request.
type VKProviderConfigCreate struct {
	Provider      string       `json:"provider"`
	Weight        float64      `json:"weight,omitempty"`
	AllowedModels []string     `json:"allowed_models,omitempty"`
	KeyIDs        []string     `json:"key_ids,omitempty"`
	Budget        *VKBudget    `json:"budget,omitempty"`
	RateLimit     *VKRateLimit `json:"rate_limit,omitempty"`
}

// VKProviderConfigResponse is a provider config entry in an API response.
type VKProviderConfigResponse struct {
	ID            uint         `json:"id"`
	Provider      string       `json:"provider"`
	Weight        *float64     `json:"weight"`
	AllowedModels []string     `json:"allowed_models"`
	Budget        *VKBudget    `json:"budget,omitempty"`
	RateLimit     *VKRateLimit `json:"rate_limit,omitempty"`
}

// VirtualKeyResponse mirrors the TableVirtualKey JSON structure returned by Bifrost.
type VirtualKeyResponse struct {
	ID              string                     `json:"id"`
	Name            string                     `json:"name"`
	Description     string                     `json:"description,omitempty"`
	Value           string                     `json:"value"`
	IsActive        bool                       `json:"is_active"`
	ProviderConfigs []VKProviderConfigResponse `json:"provider_configs"`
	TeamID          *string                    `json:"team_id,omitempty"`
	CustomerID      *string                    `json:"customer_id,omitempty"`
	Budget          *VKBudget                  `json:"budget,omitempty"`
	RateLimit       *VKRateLimit               `json:"rate_limit,omitempty"`
}

// createVKResponse wraps the create VK API response envelope.
type createVKResponse struct {
	VirtualKey VirtualKeyResponse `json:"virtual_key"`
}

// getVKResponse wraps the get VK API response envelope.
type getVKResponse struct {
	VirtualKey VirtualKeyResponse `json:"virtual_key"`
}

// CreateVirtualKeyRequest is the payload for POST /api/governance/virtual-keys.
type CreateVirtualKeyRequest struct {
	Name            string                   `json:"name"`
	Description     string                   `json:"description,omitempty"`
	ProviderConfigs []VKProviderConfigCreate `json:"provider_configs,omitempty"`
	TeamID          *string                  `json:"team_id,omitempty"`
	CustomerID      *string                  `json:"customer_id,omitempty"`
	Budget          *VKBudget                `json:"budget,omitempty"`
	RateLimit       *VKRateLimit             `json:"rate_limit,omitempty"`
	IsActive        *bool                    `json:"is_active,omitempty"`
}

// UpdateVirtualKeyRequest is the payload for PUT /api/governance/virtual-keys/{id}.
// All provider configs are sent without IDs (nil ID = new), which causes Bifrost to
// delete all existing configs and recreate them — a full replacement.
//
// team_id and customer_id intentionally lack omitempty: a nil pointer marshals
// to JSON null, which Bifrost treats as an explicit clear of any existing
// association. Omitting them would leave a previous value in place.
type UpdateVirtualKeyRequest struct {
	Name            *string                  `json:"name,omitempty"`
	Description     *string                  `json:"description,omitempty"`
	ProviderConfigs []VKProviderConfigCreate `json:"provider_configs,omitempty"`
	TeamID          *string                  `json:"team_id"`
	CustomerID      *string                  `json:"customer_id"`
	Budget          *VKUpdateBudget          `json:"budget,omitempty"`
	RateLimit       *VKRateLimit             `json:"rate_limit,omitempty"`
	IsActive        *bool                    `json:"is_active,omitempty"`
}

// CreateVirtualKey calls POST /api/governance/virtual-keys.
func (c *BifrostClient) CreateVirtualKey(ctx context.Context, req CreateVirtualKeyRequest) (*VirtualKeyResponse, error) {
	var envelope createVKResponse
	if err := c.doRequest(ctx, http.MethodPost, "/api/governance/virtual-keys", req, &envelope); err != nil {
		return nil, err
	}
	return &envelope.VirtualKey, nil
}

// GetVirtualKey calls GET /api/governance/virtual-keys/{id}.
func (c *BifrostClient) GetVirtualKey(ctx context.Context, id string) (*VirtualKeyResponse, error) {
	var envelope getVKResponse
	if err := c.doRequest(ctx, http.MethodGet, "/api/governance/virtual-keys/"+url.PathEscape(id), nil, &envelope); err != nil {
		return nil, err
	}
	return &envelope.VirtualKey, nil
}

// UpdateVirtualKey calls PUT /api/governance/virtual-keys/{id}.
func (c *BifrostClient) UpdateVirtualKey(ctx context.Context, id string, req UpdateVirtualKeyRequest) (*VirtualKeyResponse, error) {
	var envelope createVKResponse
	if err := c.doRequest(ctx, http.MethodPut, "/api/governance/virtual-keys/"+url.PathEscape(id), req, &envelope); err != nil {
		return nil, err
	}
	return &envelope.VirtualKey, nil
}

// DeleteVirtualKey calls DELETE /api/governance/virtual-keys/{id}.
func (c *BifrostClient) DeleteVirtualKey(ctx context.Context, id string) error {
	return c.doRequest(ctx, http.MethodDelete, "/api/governance/virtual-keys/"+url.PathEscape(id), nil, nil)
}
