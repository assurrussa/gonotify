package notifyhub_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/assurrussa/gonotify/transport"
	"github.com/assurrussa/gonotify/transport/notifyhub"
)

const (
	confidentialSecret            = "synthetic-confidential-secret"
	confidentialServerURL         = "https://notify.example.invalid"
	confidentialNotificationsPath = "/v1/notifications"
	confidentialPreCanceled       = "pre-canceled"
	confidentialValid             = "confidential-valid"
)

func confidentialRequest() notifyhub.ConfidentialEmailRequest {
	return notifyhub.ConfidentialEmailRequest{
		IdempotencyKey: "stable-confidential-operation",
		Event:          "auth.password_reset",
		ExpiresAt:      time.Now().Add(time.Hour),
		Email: transport.EmailMessage{
			From: "noreply@example.test", To: []string{"recipient@example.test"},
			Subject: "Password reset", Text: confidentialSecret, HTML: "<p>" + confidentialSecret + "</p>",
		},
	}
}

func confidentialReceiptJSON(req notifyhub.ConfidentialEmailRequest, status string, duplicate bool) string {
	return fmt.Sprintf(`{"id":"operation-1","status":%q,"duplicate":%t,"expires_at":%d}`,
		status, duplicate, req.ExpiresAt.Unix())
}

func confidentialTestClient(t *testing.T, handler http.HandlerFunc, prefix string) *notifyhub.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := notifyhub.New(notifyhub.Config{BaseURL: server.URL + prefix, ProjectKey: testToken})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func assertConfidentialError(
	t *testing.T, err error, outcome notifyhub.ConfidentialEmailOutcome, cause error,
) *notifyhub.ConfidentialEmailError {
	t.Helper()
	var typed *notifyhub.ConfidentialEmailError
	if !errors.As(err, &typed) || typed.Outcome != outcome {
		t.Fatalf("got %v, want outcome %s", err, outcome)
	}
	if cause != nil && !errors.Is(err, cause) {
		t.Fatalf("got %v, want cause %v", err, cause)
	}
	for _, secret := range []string{confidentialSecret, testToken, "recipient@example.test"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("error leaked confidential content")
		}
	}
	if len(err.Error()) > 250 || strings.ContainsAny(err.Error(), "\r\n") {
		t.Fatal("unbounded or multiline diagnostic")
	}
	return typed
}

func TestConfidentialWireContractAndRepeat(t *testing.T) {
	req := confidentialRequest()
	var calls int
	var firstBody string
	client := confidentialTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/prefix/v1/confidential-email" {
			t.Errorf("unexpected method or endpoint: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("Idempotency-Key") != req.IdempotencyKey {
			t.Error("existing project authentication or operation key changed")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var decoded struct {
			Event     string                 `json:"event"`
			Email     transport.EmailMessage `json:"email"`
			ExpiresAt int64                  `json:"expires_at"` //nolint:tagliatelle // HTTP contract.
		}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Error(err)
		}
		if decoded.Event != req.Event || decoded.ExpiresAt != req.ExpiresAt.Unix() ||
			decoded.Email.Text != confidentialSecret || decoded.Email.HTML != req.Email.HTML ||
			len(decoded.Email.To) != 1 || decoded.Email.To[0] != req.Email.To[0] {
			t.Error("wire payload or original token expiry changed")
		}
		if calls == 1 {
			firstBody = string(body)
		} else if string(body) != firstBody {
			t.Error("repeat changed the payload or expiry")
		}
		_, _ = io.WriteString(w, confidentialReceiptJSON(req, "accepted", calls > 1))
	}, "/prefix")
	for _, duplicate := range []bool{false, true} {
		receipt, err := client.SendConfidentialEmail(context.Background(), req)
		if err != nil || receipt.Status != notifyhub.StatusAccepted || receipt.Duplicate != duplicate ||
			receipt.ID != "operation-1" || !receipt.ExpiresAt.Equal(time.Unix(req.ExpiresAt.Unix(), 0)) {
			t.Fatalf("unexpected accepted receipt: %+v / %v", receipt, err)
		}
	}
	if calls != 2 {
		t.Fatalf("unexpected automatic retry/fallback count: %d", calls)
	}
}

func TestConfidentialOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		httpStatus int
		status     string
		outcome    notifyhub.ConfidentialEmailOutcome
		cause      error
	}{
		{"simulated", 200, "simulated", notifyhub.OutcomeSimulated, notifyhub.ErrConfidentialSimulated},
		{"pending", 202, "dispatching", notifyhub.OutcomeUnknown, notifyhub.ErrConfidentialOutcomeUnknown},
		{"unknown", 409, "unknown", notifyhub.OutcomeUnknown, notifyhub.ErrConfidentialOutcomeUnknown},
		{"temporary-rejection", 503, string(notifyhub.StatusRetry), notifyhub.OutcomeRetryable, transport.ErrTemporarilyUnavailable},
		{"failed", 422, "failed", notifyhub.OutcomeRejected, notifyhub.ErrConfidentialRejected},
		{"expired-response", 410, string(notifyhub.StatusExpired), notifyhub.OutcomeNotSent, transport.ErrExpired},
		{"bad-input", 400, "", notifyhub.OutcomeNotSent, transport.ErrInvalidRequest},
		{"unauthorized", 401, "", notifyhub.OutcomeNotSent, transport.ErrUnauthorized},
		{"forbidden", 403, "", notifyhub.OutcomeNotSent, transport.ErrUnauthorized},
		{"missing-endpoint", 404, "", notifyhub.OutcomeNotSent, transport.ErrNotFound},
		{"wrong-method", 405, "", notifyhub.OutcomeNotSent, transport.ErrNotFound},
		{"too-large", 413, "", notifyhub.OutcomeNotSent, transport.ErrPayloadTooLarge},
		{"quota", 429, "", notifyhub.OutcomeNotSent, transport.ErrQuotaExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := confidentialRequest()
			calls := 0
			client := confidentialTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Retry-After", "45")
				w.WriteHeader(tt.httpStatus)
				_, _ = io.WriteString(w, confidentialReceiptJSON(req, tt.status, true))
			}, "")
			_, err := client.SendConfidentialEmail(context.Background(), req)
			typed := assertConfidentialError(t, err, tt.outcome, tt.cause)
			if typed.StatusCode != tt.httpStatus || calls != 1 {
				t.Fatalf("wrong status or request count: %d / %d", typed.StatusCode, calls)
			}
			if (tt.httpStatus == 429 || tt.httpStatus == 503) && typed.RetryAfter != 45*time.Second {
				t.Fatalf("Retry-After lost: %v", typed.RetryAfter)
			}
		})
	}
}

func TestConfidentialUntrustedResponsesAreUnknown(t *testing.T) {
	req := confidentialRequest()
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"ordinary-queue", 202, auditReceipt},
		{"accepted-wrong-http", 202, confidentialReceiptJSON(req, "accepted", false)},
		{"delivered-not-contract", 200, confidentialReceiptJSON(req, "delivered", false)},
		{"queued-not-contract", 200, confidentialReceiptJSON(req, "queued", false)},
		{"missing-id", 200, fmt.Sprintf(`{"status":"accepted","expires_at":%d}`, req.ExpiresAt.Unix())},
		{"mismatched-expiry", 200, `{"id":"operation-1","status":"accepted","expires_at":1}`},
		{"missing-expiry", 200, `{"id":"operation-1","status":"accepted"}`},
		{"malformed", 200, confidentialSecret},
		{auditOversized, 200, strings.Repeat("x", auditMaxResponseBytes+1)},
		{"generic-unavailable", 503, `{"error":"` + confidentialSecret + `"}`},
		{"wrong-retry-http", 500, confidentialReceiptJSON(req, string(notifyhub.StatusRetry), false)},
		{"unknown-code", 409, `{"error":"` + confidentialSecret + `"}`},
		{"empty", 204, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := confidentialTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}, "")
			receipt, err := client.SendConfidentialEmail(context.Background(), req)
			_ = assertConfidentialError(t, err, notifyhub.OutcomeUnknown, notifyhub.ErrConfidentialOutcomeUnknown)
			if receipt.ID != "" || errors.Is(err, transport.ErrTemporarilyUnavailable) {
				t.Fatal("untrusted response must not look accepted or retryable")
			}
		})
	}
}

func TestConfidentialNoNetworkOnInvalidOrExpired(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*notifyhub.ConfidentialEmailRequest)
		cause  error
	}{
		{"key", func(r *notifyhub.ConfidentialEmailRequest) { r.IdempotencyKey = "short" }, transport.ErrInvalidRequest},
		{"missing-expiry", func(r *notifyhub.ConfidentialEmailRequest) { r.ExpiresAt = time.Time{} }, transport.ErrInvalidRequest},
		{"expired", func(r *notifyhub.ConfidentialEmailRequest) { r.ExpiresAt = time.Now() }, transport.ErrExpired},
		{
			"multiple", func(r *notifyhub.ConfidentialEmailRequest) { r.Email.To = append(r.Email.To, r.Email.To[0]) },
			transport.ErrInvalidRequest,
		},
		{"missing-to", func(r *notifyhub.ConfidentialEmailRequest) { r.Email.To = nil }, transport.ErrInvalidRequest},
		{
			"invalid-from", func(r *notifyhub.ConfidentialEmailRequest) { r.Email.From = confidentialSecret },
			transport.ErrInvalidRequest,
		},
		{"invalid-to", func(r *notifyhub.ConfidentialEmailRequest) { r.Email.To[0] = confidentialSecret }, transport.ErrInvalidRequest},
		{
			"empty-body", func(r *notifyhub.ConfidentialEmailRequest) { r.Email.Text, r.Email.HTML = "", "" },
			transport.ErrInvalidRequest,
		},
		{"empty-subject", func(r *notifyhub.ConfidentialEmailRequest) { r.Email.Subject = " " }, transport.ErrInvalidRequest},
		{
			"header-injection", func(r *notifyhub.ConfidentialEmailRequest) { r.Email.Subject += "\r\nBcc: secret" },
			transport.ErrInvalidRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := confidentialTestClient(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("unexpected network I/O") }, "")
			req := confidentialRequest()
			tc.change(&req)
			_, err := client.SendConfidentialEmail(context.Background(), req)
			_ = assertConfidentialError(t, err, notifyhub.OutcomeNotSent, tc.cause)
		})
	}
}

func TestConfidentialContextAndTimeouts(t *testing.T) {
	for _, mode := range []string{confidentialPreCanceled, "caller", "client", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var calls int
				client, err := notifyhub.New(notifyhub.Config{
					BaseURL: confidentialServerURL, ProjectKey: testToken, Timeout: 2 * time.Second,
					HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
						calls++
						if r.GetBody != nil {
							t.Error("ambiguous POST must not be replayable")
						}
						<-r.Context().Done()
						return nil, r.Context().Err()
					})},
				})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				req := confidentialRequest()
				outcome, cause, elapsed := notifyhub.OutcomeUnknown, context.DeadlineExceeded, 2*time.Second
				switch mode {
				case confidentialPreCanceled:
					cancel()
					outcome, cause, elapsed = notifyhub.OutcomeNotSent, context.Canceled, 0
				case "caller":
					var stop context.CancelFunc
					ctx, stop = context.WithTimeout(ctx, time.Second)
					defer stop()
					elapsed = time.Second
				case "expiry":
					req.ExpiresAt = time.Now().Add(time.Second + 900*time.Millisecond)
					elapsed = time.Second
				}
				start := time.Now()
				_, err = client.SendConfidentialEmail(ctx, req)
				_ = assertConfidentialError(t, err, outcome, cause)
				if time.Since(start) != elapsed || calls > 1 || (mode == confidentialPreCanceled && calls != 0) {
					t.Fatalf("wrong elapsed/calls: %s / %d", time.Since(start), calls)
				}
			})
		})
	}
}

func TestConfidentialRedirectBlockedAndClientUnmodified(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { redirected.Add(1) }))
	t.Cleanup(target.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	original := &http.Client{}
	client, err := notifyhub.New(notifyhub.Config{BaseURL: server.URL, ProjectKey: testToken, HTTPClient: original})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SendConfidentialEmail(context.Background(), confidentialRequest())
	_ = assertConfidentialError(t, err, notifyhub.OutcomeUnknown, notifyhub.ErrConfidentialOutcomeUnknown)
	if redirected.Load() != 0 || original.CheckRedirect != nil {
		t.Fatal("redirect followed or caller client mutated")
	}
}

func TestConfidentialNetworkErrorsAreSanitized(t *testing.T) {
	client, err := notifyhub.New(notifyhub.Config{
		BaseURL: confidentialServerURL, ProjectKey: testToken,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return nil, errors.New(confidentialSecret + ": " + testToken)
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SendConfidentialEmail(context.Background(), confidentialRequest())
	_ = assertConfidentialError(t, err, notifyhub.OutcomeUnknown, notifyhub.ErrConfidentialOutcomeUnknown)
}

func TestNewRejectsEndpointAndCredentialBearingBaseURL(t *testing.T) {
	for _, suffix := range []string{
		confidentialNotificationsPath, "/v1/notifications/", "/prefix/v1/notifications", "/prefix/v1/notifications///",
		"?token=" + confidentialSecret, "#" + confidentialSecret,
	} {
		_, err := notifyhub.New(notifyhub.Config{BaseURL: confidentialServerURL + suffix, ProjectKey: testToken})
		if err == nil || strings.Contains(err.Error(), confidentialSecret) {
			t.Fatalf("unsafe BaseURL was accepted or leaked: %v", err)
		}
	}
	for _, base := range []string{
		"https://" + confidentialSecret + "@notify.example.invalid", "https://notify.example.invalid/%" + confidentialSecret,
	} {
		_, err := notifyhub.New(notifyhub.Config{BaseURL: base, ProjectKey: testToken})
		if err == nil || strings.Contains(err.Error(), confidentialSecret) {
			t.Fatalf("unsafe BaseURL was accepted or leaked: %v", err)
		}
	}
}

func TestConfidentialIdempotencyMismatch(t *testing.T) {
	req := confidentialRequest()
	calls := 0
	client := confidentialTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Idempotency-Key") != req.IdempotencyKey {
			t.Error("operation key changed")
		}
		if calls == 1 {
			_, _ = io.WriteString(w, confidentialReceiptJSON(req, "accepted", false))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":"idempotency_conflict"}`)
	}, "")
	if _, err := client.SendConfidentialEmail(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	req.Email.Text = "different sensitive content"
	_, err := client.SendConfidentialEmail(context.Background(), req)
	_ = assertConfidentialError(t, err, notifyhub.OutcomeNotSent, transport.ErrIdempotencyConflict)
	if calls != 2 {
		t.Fatal("conflict caused an automatic retry")
	}
}

func TestConfidentialResponseBodyBoundsAndClose(t *testing.T) {
	for _, mode := range []string{confidentialValid, auditOversized, auditInterrupted, auditCanceled} {
		t.Run(mode, func(t *testing.T) {
			req := confidentialRequest()
			body := &auditBody{reader: strings.NewReader(confidentialReceiptJSON(req, "accepted", false))}
			switch mode {
			case auditOversized:
				body.reader = strings.NewReader(strings.Repeat("x", auditMaxResponseBytes+100))
			case auditInterrupted:
				body.readErr = errors.New(confidentialSecret)
			case auditCanceled:
				body.readErr = context.Canceled
			}
			_, err := auditClient(t, http.StatusOK, body).SendConfidentialEmail(context.Background(), req)
			if mode == confidentialValid {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				_ = assertConfidentialError(t, err, notifyhub.OutcomeUnknown, notifyhub.ErrConfidentialOutcomeUnknown)
				if mode == auditCanceled && !errors.Is(err, context.Canceled) {
					t.Fatal("safe context cause lost")
				}
			}
			if body.closes != 1 || body.bytesRead > auditMaxResponseBytes+1 {
				t.Fatalf("body close/bounds violated: %d / %d", body.closes, body.bytesRead)
			}
		})
	}
}

func TestConfidentialUnknownRepeatKeepsOriginalOperation(t *testing.T) {
	req := confidentialRequest()
	var originalBody string
	calls := 0
	client := confidentialTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if calls == 1 {
			originalBody = string(body)
		} else if string(body) != originalBody || r.Header.Get("Idempotency-Key") != req.IdempotencyKey {
			t.Error("unknown recovery changed operation content, key or original expiry")
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, confidentialReceiptJSON(req, "unknown", calls > 1))
	}, "")
	for _, duplicate := range []bool{false, true} {
		receipt, err := client.SendConfidentialEmail(context.Background(), req)
		_ = assertConfidentialError(t, err, notifyhub.OutcomeUnknown, notifyhub.ErrConfidentialOutcomeUnknown)
		if receipt.Duplicate != duplicate || receipt.Status != notifyhub.StatusUnknown {
			t.Fatal("unknown outcome lost")
		}
	}
	if calls != 2 {
		t.Fatal("client performed an automatic retry")
	}
}

func TestConfidentialGatewayCodeAllowlist(t *testing.T) {
	for _, code := range []string{"provider_rejected_temporary", confidentialSecret} {
		req := confidentialRequest()
		client := confidentialTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			body := strings.TrimSuffix(confidentialReceiptJSON(req, string(notifyhub.StatusRetry), false), "}") + `,"code":"` + code + `"}`
			_, _ = io.WriteString(w, body)
		}, "")
		_, err := client.SendConfidentialEmail(context.Background(), req)
		typed := assertConfidentialError(t, err, notifyhub.OutcomeRetryable, transport.ErrTemporarilyUnavailable)
		want := string(notifyhub.StatusRetry)
		if code == "provider_rejected_temporary" {
			want = code
		}
		if typed.Code != want {
			t.Fatalf("unexpected diagnostic: %s", typed.Code)
		}
	}
}

func TestConfidentialFlooredDeadlineCannotExtendTokenLife(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, err := notifyhub.New(notifyhub.Config{
			BaseURL: confidentialServerURL, ProjectKey: testToken,
			HTTPClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				t.Fatal("floored expiry reached the network")
				return nil, errors.New("unexpected I/O")
			})},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := confidentialRequest()
		req.ExpiresAt = time.Now().Add(900 * time.Millisecond)
		_, err = client.SendConfidentialEmail(context.Background(), req)
		_ = assertConfidentialError(t, err, notifyhub.OutcomeNotSent, transport.ErrExpired)
	})
}
