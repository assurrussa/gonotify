package notifyhub_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/assurrussa/outbox/outbox"

	notificationsjob "github.com/assurrussa/gonotify/interfaces/outbox/notifications"
	"github.com/assurrussa/gonotify/transport"
	"github.com/assurrussa/gonotify/transport/notifyhub"
)

const (
	auditMaxResponseBytes = 1 << 20
	auditSubmit           = "submit"
	auditGet              = "get"
	auditPing             = "ping"
	auditCanceled         = "canceled-body"
	auditInvalid          = "invalid-receipt"
	auditInterrupted      = "interrupted"
	auditOversized        = "oversized"
)
const auditReceipt = `{"id":"notification","deliveries":[{"id":"delivery","status":"queued"}]}`

type auditBody struct {
	reader    io.Reader
	readErr   error
	bytesRead int
	closes    int
}

func (b *auditBody) Read(p []byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}
	n, err := b.reader.Read(p)
	b.bytesRead += n
	return n, err
}
func (b *auditBody) Close() error { b.closes++; return nil }

func auditRequest() transport.Request {
	return transport.Request{
		IdempotencyKey: "synthetic-event",
		Telegram:       &transport.TelegramMessage{ChatID: "1", Text: "synthetic message"},
	}
}

func auditClient(t *testing.T, status int, body *auditBody) *notifyhub.Client {
	t.Helper()
	client, err := notifyhub.New(notifyhub.Config{
		BaseURL: "https://notify.example.invalid", ProjectKey: "synthetic-test-key",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{http.CanonicalHeaderKey("retry-after"): []string{"45"}},
				Body:       body,
			}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func auditCall(ctx context.Context, client *notifyhub.Client, method string) error {
	switch method {
	case auditGet:
		_, err := client.Get(ctx, "notification")
		return err
	case auditPing:
		return client.Ping(ctx)
	default:
		_, err := client.Submit(ctx, auditRequest())
		return err
	}
}

func TestAuditErrorStatusSurvivesBadBody(t *testing.T) {
	statuses := map[int]error{
		400: transport.ErrInvalidRequest, 401: transport.ErrUnauthorized, 404: transport.ErrNotFound,
		409: transport.ErrIdempotencyConflict, 413: transport.ErrPayloadTooLarge,
		429: transport.ErrQuotaExceeded, 503: transport.ErrTemporarilyUnavailable, 500: transport.ErrTemporarilyUnavailable,
	}
	for status, want := range statuses {
		for _, method := range []string{auditSubmit, auditGet} {
			for _, mode := range []string{auditOversized, auditInterrupted, auditCanceled, "malformed"} {
				t.Run(fmt.Sprintf("%s/status-%d/%s", method, status, mode), func(t *testing.T) {
					checkAuditErrorStatus(t, status, want, method, mode)
				})
			}
		}
	}
}

func checkAuditErrorStatus(t *testing.T, status int, want error, method, mode string) {
	t.Helper()
	body := &auditBody{reader: strings.NewReader("not JSON")}
	switch mode {
	case auditOversized:
		body.reader = strings.NewReader(strings.Repeat("x", auditMaxResponseBytes+100))
	case auditInterrupted:
		body.readErr = io.ErrUnexpectedEOF
	case auditCanceled:
		body.readErr = context.Canceled
	}
	err := auditCall(context.Background(), auditClient(t, status, body), method)
	if !errors.Is(err, want) {
		t.Errorf("status classification lost: got %v, want %v", err, want)
	}
	if body.readErr != nil && !errors.Is(err, body.readErr) {
		t.Errorf("read error lost: %v", err)
	}
	if status == 429 {
		var quota *transport.QuotaError
		if !errors.As(err, &quota) || quota.RetryAfter != 45*time.Second {
			t.Errorf("Retry-After lost: %v", err)
		}
	} else {
		var requestError *transport.RequestError
		if !errors.As(err, &requestError) || requestError.StatusCode != status {
			t.Errorf("HTTP status lost: %v", err)
		}
	}
	if body.closes != 1 {
		t.Errorf("body close count: %d", body.closes)
	}
	if body.bytesRead > auditMaxResponseBytes+1 {
		t.Errorf("read exceeded response limit: %d", body.bytesRead)
	}
}

func TestAuditGatewayDiagnosticsAreBoundedAndNotBodyText(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unexpected-body", 500, "synthetic-private-message\nAuthorization: Bearer synthetic-credential"},
		{"oversized-error-code", 400, `{"error":"` + strings.Repeat("x", 10000) + `"}`},
		{"multiline-error-code", 429, `{"error":"synthetic-private-message\nAuthorization: Bearer synthetic-credential"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &auditBody{reader: strings.NewReader(tc.body)}
			err := auditCall(context.Background(), auditClient(t, tc.status, body), auditSubmit)
			if err == nil {
				t.Fatal("expected gateway error")
			}
			if strings.Contains(err.Error(), "synthetic-private-message") || strings.Contains(err.Error(), "synthetic-credential") {
				t.Error("gateway response text is exposed in the returned error")
			}
			if strings.ContainsAny(err.Error(), "\r\n") || len(err.Error()) > 300 {
				t.Errorf("unbounded or multiline diagnostic: length %d", len(err.Error()))
			}
		})
	}
}

func TestAuditGatewayCodes(t *testing.T) {
	for _, tc := range []struct {
		code  string
		valid bool
	}{
		{"gateway_invalid_input", true},
		{"CODE-1.0", true},
		{strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), false},
		{"", false},
		{"two words", false},
		{"code\nnext", false},
		{"код", false},
		{"code\x1b[31m", false},
	} {
		t.Run(fmt.Sprintf("length-%d/%q", len(tc.code), tc.code), func(t *testing.T) {
			encoded, err := json.Marshal(map[string]string{"error": tc.code})
			if err != nil {
				t.Fatal(err)
			}
			err = auditCall(context.Background(), auditClient(t, 400, &auditBody{reader: strings.NewReader(string(encoded))}), auditSubmit)
			var requestError *transport.RequestError
			if !errors.As(err, &requestError) {
				t.Fatalf("expected RequestError: %v", err)
			}
			want := "invalid_request"
			if tc.valid {
				want = tc.code
			}
			if requestError.Code != want {
				t.Errorf("code = %q, want %q", requestError.Code, want)
			}
		})
	}
}

func TestAuditSuccessResponseBodiesClosed(t *testing.T) {
	for _, method := range []string{auditSubmit, auditGet, auditPing} {
		for _, mode := range []string{"valid", auditInvalid, auditOversized, auditInterrupted} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				body := &auditBody{reader: strings.NewReader(auditReceipt)}
				switch mode {
				case auditInvalid:
					body.reader = strings.NewReader("not JSON")
				case auditOversized:
					body.reader = strings.NewReader(strings.Repeat("x", auditMaxResponseBytes+100))
				case auditInterrupted:
					body.readErr = io.ErrUnexpectedEOF
				}
				err := auditCall(context.Background(), auditClient(t, http.StatusOK, body), method)
				switch {
				case method == auditPing || mode == "valid":
					if err != nil {
						t.Fatal(err)
					}
				case mode == auditInterrupted:
					if !errors.Is(err, io.ErrUnexpectedEOF) {
						t.Errorf("read error lost: %v", err)
					}
				case !errors.Is(err, transport.ErrInvalidResponse):
					t.Errorf("invalid receipt accepted: %v", err)
				}
				if body.closes != 1 {
					t.Errorf("body close count: %d", body.closes)
				}
			})
		}
	}
}

func TestAuditContextCancellationAndDeadlines(t *testing.T) {
	for _, method := range []string{auditSubmit, auditGet, auditPing} {
		for _, mode := range []string{auditCanceled, "caller-deadline", "client-timeout"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					client, err := notifyhub.New(notifyhub.Config{
						BaseURL: "https://notify.example.invalid", ProjectKey: "synthetic-test-key", Timeout: 2 * time.Second,
						HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
							<-r.Context().Done()
							return nil, r.Context().Err()
						})},
					})
					if err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					want, elapsed := context.DeadlineExceeded, 2*time.Second
					switch mode {
					case auditCanceled:
						cancel()
						want, elapsed = context.Canceled, 0
					case "caller-deadline":
						var stop context.CancelFunc
						ctx, stop = context.WithTimeout(ctx, time.Second)
						defer stop()
						elapsed = time.Second
					}
					before := time.Now()
					err = auditCall(ctx, client, method)
					if !errors.Is(err, want) {
						t.Errorf("context cause lost: %v", err)
					}
					if got := time.Since(before); got != elapsed {
						t.Errorf("elapsed = %v, want %v", got, elapsed)
					}
				})
			})
		}
	}
}

func TestAuditOutboxPreservesStatusDisposition(t *testing.T) {
	for _, status := range []int{401, 429, 413, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			body := &auditBody{readErr: io.ErrUnexpectedEOF}
			client := auditClient(t, status, body)
			payload, err := notificationsjob.MarshalPayload(notificationsjob.Payload{Request: auditRequest()})
			if err != nil {
				t.Fatal(err)
			}
			job := notificationsjob.Must(notificationsjob.NewOptions(client))
			before := time.Now()
			err = job.Handle(context.Background(), payload)
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("original read error lost: %v", err)
			}
			switch status {
			case 401:
				at, ok := outbox.DeferTime(err)
				if !ok || at.Before(before.Add(5*time.Minute)) {
					t.Errorf("401 was not deferred: %v", err)
				}
			case 429:
				at, ok := outbox.RetryTime(err)
				if !ok || at.Before(before.Add(45*time.Second)) {
					t.Errorf("429 retry was lost: %v", err)
				}
			case 413:
				if !outbox.IsPermanent(err) {
					t.Errorf("413 was not permanent: %v", err)
				}
			case 500:
				_, deferred := outbox.DeferTime(err)
				_, delayed := outbox.RetryTime(err)
				if outbox.IsPermanent(err) || deferred || delayed || !errors.Is(err, transport.ErrTemporarilyUnavailable) {
					t.Errorf("500 is not an ordinary transient failure: %v", err)
				}
			}
		})
	}
}

func TestAuditPingFailureClosesBody(t *testing.T) {
	body := &auditBody{reader: strings.NewReader("synthetic-health-error")}
	err := auditClient(t, http.StatusServiceUnavailable, body).Ping(context.Background())
	if err == nil || strings.Contains(err.Error(), "synthetic-health-error") {
		t.Fatalf("unexpected ping error: %v", err)
	}
	if body.closes != 1 {
		t.Errorf("body close count: %d", body.closes)
	}
}
