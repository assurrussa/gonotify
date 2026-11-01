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
	"testing"
	"time"

	"github.com/assurrussa/gonotify/transport"
	"github.com/assurrussa/gonotify/transport/notifyhub"
)

const queueContractProjectKey = "synthetic-key"

func TestQueuedHighPriorityStableRetryAndReconciliation(t *testing.T) {
	expires := time.Now().Add(time.Hour).Truncate(time.Second).Add(987 * time.Millisecond)
	var bodies, keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+queueContractProjectKey {
			t.Error("missing authorization")
		}
		if r.Method == http.MethodGet {
			if r.URL.Path != "/v1/notifications/notification-1" {
				t.Error("wrong reconciliation path")
			}
			_, _ = io.WriteString(w, `{"id":"notification-1","deliveries":[{"id":"delivery-1","status":"accepted"}]}`)
			return
		}
		if r.URL.Path != confidentialNotificationsPath {
			t.Error("wrong submit path")
		}
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		var payload struct {
			Priority  string `json:"priority"`
			ExpiresAt int64  `json:"expires_at"` //nolint:tagliatelle // NotifyHub wire contract uses snake_case.
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Priority != "high" || payload.ExpiresAt != expires.Unix() {
			t.Errorf("priority/absolute expiry changed: %+v, %v", payload, err)
		}
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		status := "queued"
		if len(bodies) == 3 {
			status = string(notifyhub.StatusUnknown)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"id":"notification-1","duplicate":true,"deliveries":[{"id":"delivery-1","status":%q}]}`, status)
	}))
	defer server.Close()
	client, err := notifyhub.New(notifyhub.Config{BaseURL: server.URL, ProjectKey: queueContractProjectKey})
	if err != nil {
		t.Fatal(err)
	}
	req := transport.Request{
		Event: "auth.login", Priority: transport.PriorityHigh, IdempotencyKey: "stable-operation-1", ExpiresAt: &expires,
		Email: &transport.EmailMessage{
			From: "from@example.test", To: []string{"to@example.test"}, Subject: "Login", Text: "synthetic-token",
		},
	}
	if _, err := client.Submit(context.Background(), req); !errors.Is(err, transport.ErrTemporarilyUnavailable) {
		t.Fatalf("first attempt: %v", err)
	}
	for _, want := range []notifyhub.DeliveryOutcome{notifyhub.DeliveryPending, notifyhub.DeliveryUnknown} {
		receipt, err := client.Submit(context.Background(), req)
		if err != nil || receipt.ID != "notification-1" || !receipt.Duplicate || len(receipt.Deliveries) != 1 {
			t.Fatalf("queue ownership not retained: %+v / %v", receipt, err)
		}
		if got := notifyhub.ClassifyDeliveryStatus(receipt.Deliveries[0].Status); got != want {
			t.Fatalf("outcome %s, want %s", got, want)
		}
	}
	for i := range bodies {
		if bodies[i] != bodies[0] || keys[i] != req.IdempotencyKey {
			t.Fatal("retry changed body/key")
		}
	}
	receipt, err := client.Get(context.Background(), "notification-1")
	if err != nil || notifyhub.ClassifyDeliveryStatus(receipt.Deliveries[0].Status) != notifyhub.DeliveryProviderAccepted {
		t.Fatalf("reconciliation: %+v / %v", receipt, err)
	}
}

func TestQueuePriorityRejectedBeforeIO(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("invalid request reached gateway")
	}))
	defer server.Close()
	client, err := notifyhub.New(notifyhub.Config{BaseURL: server.URL, ProjectKey: queueContractProjectKey})
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Second)
	future := time.Now().Add(time.Hour)
	for _, tc := range []struct {
		priority transport.Priority
		expiry   *time.Time
		want     error
	}{
		{"low", &future, transport.ErrInvalidRequest},
		{"HIGH", &future, transport.ErrInvalidRequest},
		{transport.PriorityHigh, nil, transport.ErrInvalidRequest},
		{transport.PriorityHigh, &expired, transport.ErrExpired},
	} {
		_, err := client.Submit(context.Background(), transport.Request{
			Priority: tc.priority, ExpiresAt: tc.expiry,
			IdempotencyKey: "operation-key", Email: &transport.EmailMessage{Text: "synthetic"},
		})
		if !errors.Is(err, tc.want) {
			t.Errorf("priority %q: %v, want %v", tc.priority, err, tc.want)
		}
	}
}

func TestClassifyQueueDeliveryStatus(t *testing.T) {
	for _, tc := range []struct {
		statuses string
		want     notifyhub.DeliveryOutcome
	}{
		{"queued sending retry", notifyhub.DeliveryPending},
		{"accepted", notifyhub.DeliveryProviderAccepted},
		{"delivered", notifyhub.DeliveryDelivered},
		{string(notifyhub.StatusSimulated), notifyhub.DeliverySimulated},
		{"failed expired suppressed", notifyhub.DeliveryFailed},
		{"unknown bounced complained dispatching future_status ACCEPTED", notifyhub.DeliveryUnknown},
	} {
		for _, status := range strings.Fields(tc.statuses) {
			if got := notifyhub.ClassifyDeliveryStatus(status); got != tc.want {
				t.Errorf("%s: %s, want %s", status, got, tc.want)
			}
		}
	}
	if notifyhub.ClassifyDeliveryStatus("") != notifyhub.DeliveryUnknown {
		t.Error("empty status is unknown")
	}
}

func TestRetiredConfidentialEndpointDoesNotQueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/confidential-email" {
			t.Error("legacy call fell back to queue")
		}
		w.WriteHeader(http.StatusGone)
	}))
	defer server.Close()
	client, err := notifyhub.New(notifyhub.Config{BaseURL: server.URL, ProjectKey: queueContractProjectKey})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SendConfidentialEmail(context.Background(), notifyhub.ConfidentialEmailRequest{
		IdempotencyKey: "legacy-operation", ExpiresAt: time.Now().Add(time.Hour),
		Email: transport.EmailMessage{From: "from@example.test", To: []string{"to@example.test"}, Subject: "Login", Text: "synthetic"},
	})
	var outcome *notifyhub.ConfidentialEmailError
	if !errors.As(err, &outcome) || outcome.Outcome != notifyhub.OutcomeNotSent || outcome.StatusCode != http.StatusGone {
		t.Fatalf("retired legacy endpoint unexpectedly succeeded: %v", err)
	}
}
