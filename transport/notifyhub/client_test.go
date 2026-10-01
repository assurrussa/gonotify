package notifyhub_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/assurrussa/gonotify/transport"
	"github.com/assurrussa/gonotify/transport/notifyhub"
)

const (
	testProjectKey   = "key"
	testSecret       = "secret"
	testToken        = "test-token"
	testEmailFrom    = "a@b.c"
	testEmailTo      = "d@e.f"
	testLocalhostURL = "http://localhost:8080"
)

func TestNew_Validation(t *testing.T) {
	tests := []struct {
		name              string
		baseURL           string
		projectKey        string
		allowInsecureHTTP bool
		wantErr           bool
	}{
		{
			name:       "empty BaseURL",
			baseURL:    "",
			projectKey: testSecret,
			wantErr:    true,
		},
		{
			name:       "empty ProjectKey",
			baseURL:    testLocalhostURL,
			projectKey: "",
			wantErr:    true,
		},
		{
			name:       "invalid scheme ftp",
			baseURL:    "ftp://localhost:8080",
			projectKey: testSecret,
			wantErr:    true,
		},
		{
			name:       "missing host",
			baseURL:    "http://",
			projectKey: testSecret,
			wantErr:    true,
		},
		{
			name:       "relative path",
			baseURL:    "/v1/api",
			projectKey: testSecret,
			wantErr:    true,
		},
		{
			name:       "valid http localhost",
			baseURL:    testLocalhostURL,
			projectKey: testSecret,
			wantErr:    false,
		},
		{
			name:       "valid http 127.0.0.1",
			baseURL:    "http://127.0.0.1:8080",
			projectKey: testSecret,
			wantErr:    false,
		},
		{
			name:       "valid http [::1]",
			baseURL:    "http://[::1]:8080",
			projectKey: testSecret,
			wantErr:    false,
		},
		{
			name:       "remote http disallowed by default",
			baseURL:    "http://notify.internal:8080",
			projectKey: testSecret,
			wantErr:    true,
		},
		{
			name:              "remote http allowed with AllowInsecureHTTP",
			baseURL:           "http://notify.internal:8080",
			projectKey:        testSecret,
			allowInsecureHTTP: true,
			wantErr:           false,
		},
		{
			name:       "valid https remote",
			baseURL:    "https://notify.example.com/api",
			projectKey: testSecret,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := notifyhub.New(notifyhub.Config{
				BaseURL:           tt.baseURL,
				ProjectKey:        tt.projectKey,
				AllowInsecureHTTP: tt.allowInsecureHTTP,
			})
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.wantErr && client == nil {
				t.Fatal("expected non-nil client")
			}
		})
	}
}

func TestSubmit_Success_202Accepted(t *testing.T) {
	var (
		gotAuth        string
		gotIdempotency string
		gotContentType string
		gotBody        map[string]any
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/notifications" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		gotIdempotency = r.Header.Get("Idempotency-Key")
		gotContentType = r.Header.Get("Content-Type")

		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{
			"id": "notif-123",
			"duplicate": false,
			"deliveries": [
				{
					"id": "del-1",
					"channel": "email",
					"recipient": "user@example.test",
					"status": "queued"
				}
			]
		}`))
	}))
	defer server.Close()

	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    server.URL,
		ProjectKey: "test-token-123",
	})
	if err != nil {
		t.Fatal(err)
	}

	expires := time.Now().Add(time.Hour).Truncate(time.Second)
	req := transport.Request{
		Event:          "user.welcome",
		IdempotencyKey: "idem-key-abc",
		ExpiresAt:      &expires,
		Email: &transport.EmailMessage{
			From:    "noreply@example.test",
			To:      []string{"user@example.test"},
			Subject: "Welcome",
			HTML:    "<p>Welcome!</p>",
			Text:    "Welcome!",
		},
	}

	receipt, err := client.Submit(context.Background(), req)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	if gotAuth != "Bearer test-token-123" {
		t.Fatalf("unexpected auth: %s", gotAuth)
	}
	if gotIdempotency != "idem-key-abc" {
		t.Fatalf("unexpected idempotency key: %s", gotIdempotency)
	}
	if gotContentType != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type: %s", gotContentType)
	}
	if gotBody["event"] != "user.welcome" {
		t.Fatalf("unexpected event: %v", gotBody["event"])
	}
	if gotBody["expires_at"] != float64(expires.Unix()) {
		t.Fatalf("unexpected expires_at: %v", gotBody["expires_at"])
	}

	if receipt.ID != "notif-123" {
		t.Fatalf("unexpected receipt id: %s", receipt.ID)
	}
	if receipt.Duplicate {
		t.Fatal("expected duplicate to be false")
	}
	if len(receipt.Deliveries) != 1 || receipt.Deliveries[0].ID != "del-1" {
		t.Fatalf("unexpected deliveries: %#v", receipt.Deliveries)
	}
	if receipt.Deliveries[0].Status != string(notifyhub.StatusQueued) {
		t.Fatalf("unexpected delivery status: %s", receipt.Deliveries[0].Status)
	}
}

func TestSubmit_SubpathPrefixBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/notifyhub/v1/notifications" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "notif-subpath-1",
			"duplicate": false,
			"deliveries": [{"id": "d1", "channel": "email", "status": "delivered"}]
		}`))
	}))
	defer server.Close()

	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    server.URL + "/notifyhub",
		ProjectKey: testToken,
	})
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := client.Submit(context.Background(), transport.Request{
		Event:          "subpath.event",
		IdempotencyKey: "idem-subpath-test",
		Email:          &transport.EmailMessage{From: testEmailFrom, To: []string{testEmailTo}, Subject: "s", Text: "t"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt.ID != "notif-subpath-1" {
		t.Fatalf("unexpected receipt id: %s", receipt.ID)
	}
}

func TestSubmit_DisallowsRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/notifications" {
			http.Redirect(w, r, "/somewhere-else", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": "leaked", "deliveries": [{"id": "d", "status": "delivered"}]}`))
	}))
	defer server.Close()

	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    server.URL,
		ProjectKey: "secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Submit(context.Background(), transport.Request{
		Event:          "test.redirect",
		IdempotencyKey: "idem-redirect-test",
		Email:          &transport.EmailMessage{From: testEmailFrom, To: []string{testEmailTo}, Subject: "s", Text: "t"},
	})
	if err == nil {
		t.Fatal("expected error when server redirects")
	}
	var reqErr *transport.RequestError
	if !errors.As(err, &reqErr) {
		t.Fatalf("expected RequestError, got: %T", err)
	}
	if reqErr.StatusCode != http.StatusFound {
		t.Fatalf("expected status 302, got %d", reqErr.StatusCode)
	}
}

func TestSubmit_InvalidResponse(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "empty json object",
			response: `{}`,
		},
		{
			name:     "missing id",
			response: `{"deliveries": [{"id": "d1", "status": "delivered"}]}`,
		},
		{
			name:     "empty deliveries",
			response: `{"id": "notif-1", "deliveries": []}`,
		},
		{
			name:     "delivery missing id",
			response: `{"id": "notif-1", "deliveries": [{"channel": "email", "status": "delivered"}]}`,
		},
		{
			name:     "delivery missing status",
			response: `{"id": "notif-1", "deliveries": [{"id": "d1", "channel": "email"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client, err := notifyhub.New(notifyhub.Config{
				BaseURL:    server.URL,
				ProjectKey: testToken,
			})
			if err != nil {
				t.Fatal(err)
			}

			_, err = client.Submit(context.Background(), transport.Request{
				Event:          "test",
				IdempotencyKey: "idem-invalid-resp",
				Email:          &transport.EmailMessage{From: testEmailFrom, To: []string{testEmailTo}, Subject: "s", Text: "t"},
			})
			if err == nil {
				t.Fatal("expected ErrInvalidResponse, got nil")
			}
			if !errors.Is(err, transport.ErrInvalidResponse) {
				t.Fatalf("expected ErrInvalidResponse, got %v", err)
			}
		})
	}
}

func TestSubmit_Duplicate_200OK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "notif-dup-456",
			"duplicate": true,
			"deliveries": [
				{
					"id": "del-dup",
					"channel": "telegram",
					"recipient": "12345678",
					"status": "delivered"
				}
			]
		}`))
	}))
	defer server.Close()

	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    server.URL,
		ProjectKey: testToken,
	})
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := client.Submit(context.Background(), transport.Request{
		Event:          "alert",
		IdempotencyKey: "idem-duplicate-test",
		Telegram: &transport.TelegramMessage{
			ChatID: "12345678",
			Text:   "hello",
		},
	})
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}
	if !receipt.Duplicate {
		t.Fatal("expected duplicate to be true")
	}
	if receipt.ID != "notif-dup-456" {
		t.Fatalf("unexpected receipt id: %s", receipt.ID)
	}
	if receipt.Deliveries[0].Status != string(notifyhub.StatusDelivered) {
		t.Fatalf("unexpected delivery status: %s", receipt.Deliveries[0].Status)
	}
}

func TestSubmit_EmptyPayload_Validation(t *testing.T) {
	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    "http://localhost",
		ProjectKey: testToken,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Submit(context.Background(), transport.Request{
		Event:          "empty",
		IdempotencyKey: "idem-empty-test",
	})
	if err == nil {
		t.Fatal("expected validation error for request with no email and no telegram")
	}
	if !errors.Is(err, transport.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got: %v", err)
	}
}

func runErrorCodeTest(
	t *testing.T,
	statusCode int,
	responseBody string,
	headers map[string]string,
	wantErr error,
	checkErr func(t *testing.T, err error),
) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(responseBody))
	}))
	defer server.Close()

	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    server.URL,
		ProjectKey: testProjectKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Submit(context.Background(), transport.Request{
		Event:          "test",
		IdempotencyKey: "idem-err-code-test",
		Telegram: &transport.TelegramMessage{
			ChatID: "123",
			Text:   "msg",
		},
	})
	if err == nil {
		t.Fatalf("expected error for status %d", statusCode)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error wrapping %v, got: %v", wantErr, err)
	}
	if checkErr != nil {
		checkErr(t, err)
	}
}

func TestSubmit_ErrorCodes(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		headers      map[string]string
		wantErr      error
		checkErr     func(t *testing.T, err error)
	}{
		{
			name:         "400 Bad Request",
			statusCode:   http.StatusBadRequest,
			responseBody: `{"error": "invalid_mime_headers"}`,
			wantErr:      transport.ErrInvalidRequest,
			checkErr: func(t *testing.T, err error) {
				t.Helper()
				var reqErr *transport.RequestError
				if !errors.As(err, &reqErr) {
					t.Fatalf("expected RequestError, got: %T", err)
				}
				if reqErr.Code != "invalid_mime_headers" {
					t.Fatalf("expected code invalid_mime_headers, got %s", reqErr.Code)
				}
			},
		},
		{
			name:         "401 Unauthorized",
			statusCode:   http.StatusUnauthorized,
			responseBody: `{"error": "unauthorized"}`,
			wantErr:      transport.ErrUnauthorized,
		},
		{
			name:         "409 Conflict",
			statusCode:   http.StatusConflict,
			responseBody: `{"error": "idempotency_conflict"}`,
			wantErr:      transport.ErrIdempotencyConflict,
		},
		{
			name:         "413 Request Entity Too Large",
			statusCode:   http.StatusRequestEntityTooLarge,
			responseBody: `{"error": "request_too_large"}`,
			wantErr:      transport.ErrPayloadTooLarge,
		},
		{
			name:         "429 Quota Exceeded with seconds Retry-After",
			statusCode:   http.StatusTooManyRequests,
			responseBody: `{"error": "admission_quota_exceeded"}`,
			headers:      map[string]string{"Retry-After": "45"},
			wantErr:      transport.ErrQuotaExceeded,
			checkErr: func(t *testing.T, err error) {
				t.Helper()
				var qErr *transport.QuotaError
				if !errors.As(err, &qErr) {
					t.Fatalf("expected QuotaError, got: %T", err)
				}
				if qErr.RetryAfter != 45*time.Second {
					t.Fatalf("expected 45s RetryAfter, got: %v", qErr.RetryAfter)
				}
				if qErr.Code != "admission_quota_exceeded" {
					t.Fatalf("expected admission_quota_exceeded, got: %s", qErr.Code)
				}
			},
		},
		{
			name:         "503 Service Unavailable",
			statusCode:   http.StatusServiceUnavailable,
			responseBody: `{"error": "temporarily_unavailable"}`,
			wantErr:      transport.ErrTemporarilyUnavailable,
		},
		{
			name:         "500 Internal Server Error with large body",
			statusCode:   http.StatusInternalServerError,
			responseBody: string(make([]byte, 1000)),
			wantErr:      transport.ErrTemporarilyUnavailable,
			checkErr: func(t *testing.T, err error) {
				t.Helper()
				var reqErr *transport.RequestError
				if !errors.As(err, &reqErr) {
					t.Fatalf("expected RequestError, got %T", err)
				}
				if len(reqErr.Message) > 300 {
					t.Fatalf("diagnostic message was not truncated: len %d", len(reqErr.Message))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runErrorCodeTest(t, tt.statusCode, tt.responseBody, tt.headers, tt.wantErr, tt.checkErr)
		})
	}
}

func TestGet_SuccessAndNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/notifications/exists" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": "exists",
				"duplicate": false,
				"deliveries": [{"id": "d1", "channel": "email", "status": "delivered"}]
			}`))
			return
		}
		if r.URL.Path == "/v1/notifications/missing" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "not_found"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    server.URL,
		ProjectKey: testProjectKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := client.Get(context.Background(), "exists")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt.ID != "exists" || len(receipt.Deliveries) != 1 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	_, err = client.Get(context.Background(), "missing")
	if !errors.Is(err, transport.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestGet_TypedErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    error
	}{
		{
			name:       "401 Unauthorized mapped correctly in Get",
			statusCode: http.StatusUnauthorized,
			body:       `{"error": "unauthorized"}`,
			wantErr:    transport.ErrUnauthorized,
		},
		{
			name:       "429 QuotaExceeded mapped in Get",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error": "quota_exceeded"}`,
			wantErr:    transport.ErrQuotaExceeded,
		},
		{
			name:       "503 TemporarilyUnavailable in Get",
			statusCode: http.StatusServiceUnavailable,
			body:       `{"error": "temporarily_unavailable"}`,
			wantErr:    transport.ErrTemporarilyUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client, err := notifyhub.New(notifyhub.Config{
				BaseURL:    server.URL,
				ProjectKey: testProjectKey,
			})
			if err != nil {
				t.Fatal(err)
			}

			_, err = client.Get(context.Background(), "notif-id")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestGet_InvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    server.URL,
		ProjectKey: testProjectKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Get(context.Background(), "some-id")
	if !errors.Is(err, transport.ErrInvalidResponse) {
		t.Fatalf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestPing_SuccessAndFailure(t *testing.T) {
	healthy := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		if healthy {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status": "ok"}`))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status": "unavailable"}`))
		}
	}))
	defer server.Close()

	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    server.URL,
		ProjectKey: testProjectKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("expected healthy ping: %v", err)
	}

	healthy = false
	if err := client.Ping(context.Background()); err == nil {
		t.Fatal("expected ping error when service unavailable")
	}
}

func TestSubmit_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    server.URL,
		ProjectKey: testProjectKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = client.Submit(ctx, transport.Request{
		Event:          "canceled",
		IdempotencyKey: "idem-cancel-test",
		Telegram:       &transport.TelegramMessage{ChatID: "1", Text: "text"},
	})
	if err == nil {
		t.Fatal("expected error on canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestSubmit_IdempotencyKey_Validation(t *testing.T) {
	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    testLocalhostURL,
		ProjectKey: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		key  string
	}{
		{
			name: "empty key",
			key:  "",
		},
		{
			name: "key too short (<8)",
			key:  "short",
		},
		{
			name: "key too long (>200)",
			key:  strings.Repeat("a", 201),
		},
		{
			name: "key with newline",
			key:  "valid-prefix\nrest",
		},
		{
			name: "key with carriage return",
			key:  "valid-prefix\rrest",
		},
		{
			name: "key with null byte",
			key:  "valid-prefix\x00rest",
		},
		{
			name: "key with leading whitespace",
			key:  "  valid-prefix-123",
		},
		{
			name: "key with trailing whitespace",
			key:  "valid-prefix-123  ",
		},
		{
			name: "key with outer whitespace padding",
			key:  strings.Repeat(" ", 100) + strings.Repeat("a", 150),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Submit(context.Background(), transport.Request{
				IdempotencyKey: tt.key,
				Email:          &transport.EmailMessage{From: "a@b.c", To: []string{"d@e.f"}},
			})
			if err == nil {
				t.Fatal("expected error for invalid idempotency key, got nil")
			}
			if !errors.Is(err, transport.ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got: %v", err)
			}
		})
	}
}

func TestSubmit_ResponseTooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Write 1 MiB + 10 bytes
		large := make([]byte, (1<<20)+10)
		for i := range large {
			large[i] = ' '
		}
		_, _ = w.Write(large)
	}))
	defer server.Close()

	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    server.URL,
		ProjectKey: testToken,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Submit(context.Background(), transport.Request{
		IdempotencyKey: "idem-too-large-resp",
		Email:          &transport.EmailMessage{From: testEmailFrom, To: []string{testEmailTo}},
	})
	if err == nil {
		t.Fatal("expected ErrInvalidResponse for response exceeding 1 MiB, got nil")
	}
	if !errors.Is(err, transport.ErrInvalidResponse) {
		t.Fatalf("expected ErrInvalidResponse, got: %v", err)
	}
}

func TestGet_ReceiptValidation_Deliveries(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "empty deliveries",
			response: `{"id": "notif-1", "deliveries": []}`,
		},
		{
			name:     "delivery missing id",
			response: `{"id": "notif-1", "deliveries": [{"channel": "email", "status": "delivered"}]}`,
		},
		{
			name:     "delivery missing status",
			response: `{"id": "notif-1", "deliveries": [{"id": "d1", "channel": "email"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client, err := notifyhub.New(notifyhub.Config{
				BaseURL:    server.URL,
				ProjectKey: testProjectKey,
			})
			if err != nil {
				t.Fatal(err)
			}

			_, err = client.Get(context.Background(), "notif-1")
			if err == nil {
				t.Fatal("expected ErrInvalidResponse, got nil")
			}
			if !errors.Is(err, transport.ErrInvalidResponse) {
				t.Fatalf("expected ErrInvalidResponse, got: %v", err)
			}
		})
	}
}

func TestGet_ResponseTooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		large := make([]byte, (1<<20)+10)
		for i := range large {
			large[i] = ' '
		}
		_, _ = w.Write(large)
	}))
	defer server.Close()

	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    server.URL,
		ProjectKey: testProjectKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Get(context.Background(), "some-id")
	if err == nil {
		t.Fatal("expected ErrInvalidResponse for Get response exceeding 1 MiB, got nil")
	}
	if !errors.Is(err, transport.ErrInvalidResponse) {
		t.Fatalf("expected ErrInvalidResponse, got: %v", err)
	}
}
