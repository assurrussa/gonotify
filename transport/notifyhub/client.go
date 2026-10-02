package notifyhub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/assurrussa/gonotify/transport"
)

const (
	defaultTimeout    = 10 * time.Second
	maxResponseBytes  = 1 << 20 // 1 MiB limit for defense against unbounded memory consumption
	maxErrorCodeBytes = 64
	maxRetryAfter     = time.Duration(1<<63 - 1)
)

// DeliveryStatus represents the delivery status reported by NotifyHub.
type DeliveryStatus string

const (
	StatusQueued     DeliveryStatus = "queued"
	StatusSending    DeliveryStatus = "sending"
	StatusRetry      DeliveryStatus = "retry"
	StatusAccepted   DeliveryStatus = "accepted"
	StatusSimulated  DeliveryStatus = "simulated"
	StatusDelivered  DeliveryStatus = "delivered"
	StatusFailed     DeliveryStatus = "failed"
	StatusUnknown    DeliveryStatus = "unknown"
	StatusExpired    DeliveryStatus = "expired"
	StatusBounced    DeliveryStatus = "bounced"
	StatusComplained DeliveryStatus = "complained"
	StatusSuppressed DeliveryStatus = "suppressed"
)

var (
	_ transport.Transport     = (*Client)(nil)
	_ transport.ReceiptReader = (*Client)(nil)
	_ transport.HealthChecker = (*Client)(nil)
)

// Config configures the NotifyHub transport client.
type Config struct {
	BaseURL string
	// ProjectKey is used exactly as supplied; whitespace and ASCII controls are rejected.
	ProjectKey        string
	Timeout           time.Duration
	HTTPClient        *http.Client
	AllowInsecureHTTP bool
}

// Client delivers notifications to a NotifyHub gateway instance.
type Client struct {
	baseURL    *url.URL
	projectKey string
	timeout    time.Duration
	httpClient *http.Client
}

// New creates a new NotifyHub transport client.
func New(cfg Config) (*Client, error) {
	trimmedBaseURL := strings.TrimSpace(cfg.BaseURL)
	if trimmedBaseURL == "" {
		return nil, errors.New("notifyhub: BaseURL is required")
	}
	if strings.TrimSpace(cfg.ProjectKey) == "" {
		return nil, errors.New("notifyhub: ProjectKey is required")
	}
	if cfg.ProjectKey != strings.TrimSpace(cfg.ProjectKey) {
		return nil, errors.New("notifyhub: ProjectKey must not contain surrounding whitespace")
	}
	for i := range len(cfg.ProjectKey) {
		if cfg.ProjectKey[i] <= 0x20 || cfg.ProjectKey[i] == 0x7f {
			return nil, errors.New("notifyhub: ProjectKey contains whitespace or invalid control characters")
		}
	}

	parsedURL, err := url.Parse(strings.TrimRight(trimmedBaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("notifyhub: invalid BaseURL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("notifyhub: invalid BaseURL scheme %q; must be http or https", parsedURL.Scheme)
	}
	if parsedURL.Host == "" {
		return nil, errors.New("notifyhub: BaseURL must include a host")
	}
	if parsedURL.Scheme == "http" && !cfg.AllowInsecureHTTP {
		hostname := parsedURL.Hostname()
		if !isLoopbackHost(hostname) {
			return nil, fmt.Errorf("notifyhub: insecure http scheme for remote host %q requires AllowInsecureHTTP=true", hostname)
		}
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	var httpClient *http.Client
	if cfg.HTTPClient != nil {
		cloned := *cfg.HTTPClient
		httpClient = &cloned
	} else {
		httpClient = &http.Client{Timeout: timeout}
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &Client{
		baseURL:    parsedURL,
		projectKey: cfg.ProjectKey,
		timeout:    timeout,
		httpClient: httpClient,
	}, nil
}

func isLoopbackHost(hostname string) bool {
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) endpoint(subpath string) string {
	basePath := strings.TrimRight(c.baseURL.Path, "/")
	cleanSubpath := "/" + strings.TrimLeft(subpath, "/")
	targetPath := path.Join(basePath, cleanSubpath)
	ref := &url.URL{
		Path: targetPath,
	}
	return c.baseURL.ResolveReference(ref).String()
}

type hubEmailPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text,omitempty"`
	HTML    string   `json:"html,omitempty"`
}

//nolint:tagliatelle // Wire format requires snake_case for NotifyHub HTTP API.
type hubTelegramPayload struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

//nolint:tagliatelle // Wire format requires snake_case for NotifyHub HTTP API.
type hubRequestPayload struct {
	Event     string              `json:"event"`
	Email     *hubEmailPayload    `json:"email,omitempty"`
	Telegram  *hubTelegramPayload `json:"telegram,omitempty"`
	ExpiresAt int64               `json:"expires_at,omitempty"`
}

type hubReceiptDelivery struct {
	ID        string `json:"id"`
	Channel   string `json:"channel"`
	Recipient string `json:"recipient"`
	Status    string `json:"status"`
}

type hubReceiptResponse struct {
	ID         string               `json:"id"`
	Duplicate  bool                 `json:"duplicate"`
	Deliveries []hubReceiptDelivery `json:"deliveries"`
}

type hubErrorResponse struct {
	Error string `json:"error"`
}

// Submit sends a notification to NotifyHub.
func (c *Client) Submit(ctx context.Context, req transport.Request) (transport.Receipt, error) {
	var emptyReceipt transport.Receipt

	if err := transport.ValidateIdempotencyKey(req.IdempotencyKey); err != nil {
		return emptyReceipt, err
	}

	if req.Email == nil && req.Telegram == nil {
		return emptyReceipt, fmt.Errorf("%w: at least Email or Telegram payload must be specified", transport.ErrInvalidRequest)
	}

	payload := hubRequestPayload{
		Event: req.Event,
	}

	if req.ExpiresAt != nil {
		payload.ExpiresAt = req.ExpiresAt.Unix()
	}

	if req.Email != nil {
		payload.Email = &hubEmailPayload{
			From:    req.Email.From,
			To:      req.Email.To,
			Subject: req.Email.Subject,
			Text:    req.Email.Text,
			HTML:    req.Email.HTML,
		}
	}

	if req.Telegram != nil {
		payload.Telegram = &hubTelegramPayload{
			ChatID: req.Telegram.ChatID,
			Text:   req.Telegram.Text,
		}
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return emptyReceipt, fmt.Errorf("notifyhub: marshal request: %w", err)
	}

	opCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	endpoint := c.endpoint("/v1/notifications")
	httpReq, err := http.NewRequestWithContext(opCtx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return emptyReceipt, fmt.Errorf("notifyhub: create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpReq.Header.Set("Authorization", "Bearer "+c.projectKey)
	httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)
	// NotifyHub uses whole Unix seconds; check its wire deadline immediately before I/O.
	if req.ExpiresAt != nil && payload.ExpiresAt <= time.Now().Unix() {
		return emptyReceipt, transport.ErrExpired
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return emptyReceipt, fmt.Errorf("notifyhub: request failed: %w", err)
	}
	defer resp.Body.Close()

	limitedBody := io.LimitReader(resp.Body, maxResponseBytes+1)
	respBytes, err := io.ReadAll(limitedBody)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		// Status and headers remain authoritative if an optional error body is incomplete or oversized.
		if err != nil {
			return emptyReceipt, fmt.Errorf("%w: read error response: %w", mapHTTPError(resp.StatusCode, resp.Header, nil), err)
		}
		if len(respBytes) > maxResponseBytes {
			respBytes = nil
		}
		return emptyReceipt, mapHTTPError(resp.StatusCode, resp.Header, respBytes)
	}
	if err != nil {
		return emptyReceipt, fmt.Errorf("notifyhub: read response: %w", err)
	}
	if len(respBytes) > maxResponseBytes {
		return emptyReceipt, fmt.Errorf("%w: response exceeds maximum size of %d bytes", transport.ErrInvalidResponse, maxResponseBytes)
	}

	receipt, err := decodeReceipt(respBytes)
	if err != nil {
		return emptyReceipt, fmt.Errorf("notifyhub: %w", err)
	}
	return receipt, nil
}

// Get retrieves an existing notification receipt by its ID.
func (c *Client) Get(ctx context.Context, id string) (transport.Receipt, error) {
	var emptyReceipt transport.Receipt

	if strings.TrimSpace(id) == "" || id == "." || id == ".." {
		return emptyReceipt, fmt.Errorf("%w: id must be non-empty and not a dot path segment", transport.ErrInvalidRequest)
	}

	opCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	endpoint := c.endpoint("/v1/notifications") + "/" + url.PathEscape(id)
	httpReq, err := http.NewRequestWithContext(opCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return emptyReceipt, fmt.Errorf("notifyhub: create get request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.projectKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return emptyReceipt, fmt.Errorf("notifyhub: get request failed: %w", err)
	}
	defer resp.Body.Close()

	limitedBody := io.LimitReader(resp.Body, maxResponseBytes+1)
	respBytes, err := io.ReadAll(limitedBody)
	if resp.StatusCode != http.StatusOK {
		// Status and headers remain authoritative if an optional error body is incomplete or oversized.
		if err != nil {
			return emptyReceipt, fmt.Errorf("%w: read error response: %w", mapHTTPError(resp.StatusCode, resp.Header, nil), err)
		}
		if len(respBytes) > maxResponseBytes {
			respBytes = nil
		}
		return emptyReceipt, mapHTTPError(resp.StatusCode, resp.Header, respBytes)
	}
	if err != nil {
		return emptyReceipt, fmt.Errorf("notifyhub: read get response: %w", err)
	}
	if len(respBytes) > maxResponseBytes {
		return emptyReceipt, fmt.Errorf("%w: response exceeds maximum size of %d bytes", transport.ErrInvalidResponse, maxResponseBytes)
	}

	receipt, err := decodeReceipt(respBytes)
	if err != nil {
		return emptyReceipt, fmt.Errorf("notifyhub: %w", err)
	}
	return receipt, nil
}

func decodeReceipt(respBytes []byte) (transport.Receipt, error) {
	var hubResp hubReceiptResponse
	if err := json.Unmarshal(respBytes, &hubResp); err != nil {
		return transport.Receipt{}, fmt.Errorf("%w: decode receipt response: %w", transport.ErrInvalidResponse, err)
	}

	if strings.TrimSpace(hubResp.ID) == "" {
		return transport.Receipt{}, fmt.Errorf("%w: missing notification id in response", transport.ErrInvalidResponse)
	}
	if len(hubResp.Deliveries) == 0 {
		return transport.Receipt{}, fmt.Errorf("%w: empty deliveries in response", transport.ErrInvalidResponse)
	}
	deliveries := make([]transport.DeliveryReceipt, len(hubResp.Deliveries))
	for i, d := range hubResp.Deliveries {
		if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Status) == "" {
			return transport.Receipt{}, fmt.Errorf("%w: invalid delivery item in response", transport.ErrInvalidResponse)
		}
		deliveries[i] = transport.DeliveryReceipt{
			ID:        d.ID,
			Channel:   d.Channel,
			Recipient: d.Recipient,
			Status:    d.Status,
		}
	}

	return transport.Receipt{
		ID:         hubResp.ID,
		Duplicate:  hubResp.Duplicate,
		Deliveries: deliveries,
	}, nil
}

// Ping checks gateway health via /healthz.
func (c *Client) Ping(ctx context.Context) error {
	opCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	endpoint := c.endpoint("/healthz")
	httpReq, err := http.NewRequestWithContext(opCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("notifyhub: create health request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("notifyhub: health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("notifyhub: health check returned status %d", resp.StatusCode)
	}
	return nil
}

func mapHTTPError(statusCode int, header http.Header, bodyBytes []byte) error {
	switch statusCode {
	case http.StatusBadRequest:
		errCode := parseErrorCode(bodyBytes, "invalid_request")
		return &transport.RequestError{
			StatusCode: statusCode,
			Code:       errCode,
			Message:    "request was rejected by NotifyHub",
			Err:        transport.ErrInvalidRequest,
		}

	case http.StatusUnauthorized:
		return &transport.RequestError{
			StatusCode: statusCode,
			Code:       "unauthorized",
			Message:    "invalid or missing project key",
			Err:        transport.ErrUnauthorized,
		}

	case http.StatusNotFound:
		return &transport.RequestError{
			StatusCode: statusCode,
			Code:       parseErrorCode(bodyBytes, "not_found"),
			Message:    "notification was not found",
			Err:        transport.ErrNotFound,
		}

	case http.StatusConflict:
		return &transport.RequestError{
			StatusCode: statusCode,
			Code:       "idempotency_conflict",
			Message:    "idempotency key reused with different request payload",
			Err:        transport.ErrIdempotencyConflict,
		}

	case http.StatusRequestEntityTooLarge:
		return &transport.RequestError{
			StatusCode: statusCode,
			Code:       "request_too_large",
			Message:    "notification payload exceeds maximum message size",
			Err:        transport.ErrPayloadTooLarge,
		}

	case http.StatusTooManyRequests:
		retryAfter := parseRetryAfter(header.Get("Retry-After"))
		errCode := parseErrorCode(bodyBytes, "quota_exceeded")
		return &transport.QuotaError{
			Code:       errCode,
			RetryAfter: retryAfter,
		}

	case http.StatusServiceUnavailable:
		errCode := parseErrorCode(bodyBytes, "temporarily_unavailable")
		return &transport.RequestError{
			StatusCode: statusCode,
			Code:       errCode,
			Message:    "NotifyHub is temporarily unavailable",
			Err:        transport.ErrTemporarilyUnavailable,
		}

	default:
		return &transport.RequestError{
			StatusCode: statusCode,
			Code:       fmt.Sprintf("http_%d", statusCode),
			Message:    fmt.Sprintf("unexpected status code %d", statusCode),
			Err:        transport.ErrTemporarilyUnavailable,
		}
	}
}

func parseErrorCode(data []byte, defaultCode string) string {
	var errResp hubErrorResponse
	if err := json.Unmarshal(data, &errResp); err == nil && validErrorCode(errResp.Error) {
		return errResp.Error
	}
	return defaultCode
}

func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(h, 10, 64); err == nil && seconds >= 0 {
		if seconds > int64(maxRetryAfter/time.Second) {
			return maxRetryAfter
		}
		return time.Duration(seconds) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

// Gateway codes are bounded machine identifiers, never free-form response diagnostics.
func validErrorCode(code string) bool {
	if len(code) == 0 || len(code) > maxErrorCodeBytes {
		return false
	}
	for _, ch := range code {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '.' {
			continue
		}
		return false
	}
	return true
}
