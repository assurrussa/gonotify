package notifyhub_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/assurrussa/gonotify/transport"
	"github.com/assurrussa/gonotify/transport/notifyhub"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNew_ProjectKeyRejectsWhitespaceAndControls(t *testing.T) {
	keys := []string{" secret", "secret ", "sec ret", "\tsecret", "secret\n", "secret\r", "secret\x00"}
	for b := byte(0); b <= 0x7f; b++ {
		if b < 0x20 || b == 0x7f {
			keys = append(keys, "sec"+string([]byte{b})+"ret")
		}
	}
	for i, key := range keys {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			client, err := notifyhub.New(notifyhub.Config{BaseURL: testLocalhostURL, ProjectKey: key})
			if client != nil || err == nil {
				t.Fatal("invalid project key must fail during construction")
			}
			if strings.Contains(err.Error(), key) {
				t.Fatal("validation error must not expose project key")
			}
		})
	}
}

func TestSubmit_ExpiredBeforeNetwork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, err := notifyhub.New(notifyhub.Config{
			BaseURL:    testLocalhostURL,
			ProjectKey: testToken,
			HTTPClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				t.Fatal("expired requests must not reach the network")
				return nil, errors.New("unexpected request")
			})},
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, deadline := range []time.Time{{}, time.Unix(0, 0), time.Now(), time.Now().Add(time.Millisecond)} {
			_, err := client.Submit(context.Background(), transport.Request{
				IdempotencyKey: "expiration-key",
				ExpiresAt:      &deadline,
				Telegram:       &transport.TelegramMessage{ChatID: "123", Text: "OTP"},
			})
			if !errors.Is(err, transport.ErrExpired) {
				t.Fatalf("expected ErrExpired, got %v", err)
			}
		}
	})
}

func TestGet_IDEscapedOnce(t *testing.T) {
	const id = "receipt/with%bytes?"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/prefix/v1/notifications/receipt%2Fwith%25bytes%3F" {
			t.Errorf("unexpected escaped path: %s", r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`{"id":"receipt/with%bytes?","deliveries":[{"id":"delivery","status":"queued"}]}`))
	}))
	defer server.Close()
	client, err := notifyhub.New(notifyhub.Config{BaseURL: server.URL + "/prefix", ProjectKey: testToken})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.Get(context.Background(), id)
	if err != nil || receipt.ID != id {
		t.Fatalf("unexpected receipt/error: %+v / %v", receipt, err)
	}
}

func TestGet_RejectsDotPathIDs(t *testing.T) {
	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    testLocalhostURL,
		ProjectKey: testToken,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			t.Fatal("dot path IDs must not reach the network")
			return nil, errors.New("unexpected request")
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{".", ".."} {
		_, err := client.Get(context.Background(), id)
		if !errors.Is(err, transport.ErrInvalidRequest) {
			t.Fatalf("expected invalid request, got %v", err)
		}
	}
}

func TestSubmit_RetryAfterBoundaries(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{header: "9223372036854775807", want: time.Duration(1<<63 - 1)},
		{header: "9223372036", want: 9223372036 * time.Second},
		{header: "0", want: 0},
		{header: "-1", want: 0},
		{header: "invalid", want: 0},
	} {
		t.Run(tc.header, func(t *testing.T) {
			runErrorCodeTest(t, http.StatusTooManyRequests, `{"error":"quota_exceeded"}`,
				map[string]string{"Retry-After": tc.header}, transport.ErrQuotaExceeded, func(t *testing.T, err error) {
					t.Helper()
					var quota *transport.QuotaError
					if !errors.As(err, &quota) || quota.RetryAfter != tc.want {
						t.Fatalf("unexpected RetryAfter: %v", err)
					}
				})
		})
	}
}
