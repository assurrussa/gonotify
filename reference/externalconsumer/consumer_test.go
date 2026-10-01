package externalconsumer_test

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/assurrussa/gonotify"
	notificationsjob "github.com/assurrussa/gonotify/interfaces/outbox/notifications"
	"github.com/assurrussa/gonotify/templates"
	"github.com/assurrussa/gonotify/transport"
	"github.com/assurrussa/gonotify/transport/notifyhub"
)

type dummyTransport struct{}

func TestConsumerRootContract(t *testing.T) {
	if err := gonotify.ValidateIdempotencyKey("consumer-key"); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(gonotify.ValidateIdempotencyKey("short"), transport.ErrInvalidRequest) {
		t.Fatal("root validator must preserve transport error semantics")
	}
	if !errors.Is(gonotify.ErrExpired, transport.ErrExpired) {
		t.Fatal("root expiration error must match transport")
	}
}

func (d *dummyTransport) Submit(_ context.Context, _ transport.Request) (transport.Receipt, error) {
	return transport.Receipt{ID: "msg-test"}, nil
}

// This file is also copied into a separate module by scripts/public-consumer.sh.
// Keep imports limited to the supported consumer surface and public dependencies.
func TestConsumerTemplatesSmoke(t *testing.T) {
	testFS := fstest.MapFS{
		"account/welcome/html.tmpl":    &fstest.MapFile{Data: []byte("<p>Hello, {{ .Name }}!</p>")},
		"account/welcome/txt.tmpl":     &fstest.MapFile{Data: []byte("Hello, {{ .Name }}!")},
		"account/welcome/subject.tmpl": &fstest.MapFile{Data: []byte("Welcome, {{ .Name }}")},
	}
	renderer := templates.New(testFS)
	content, err := renderer.Render("account/welcome", map[string]any{"Name": "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if content.Subject != "Welcome, Alice" {
		t.Fatalf("unexpected subject: %q", content.Subject)
	}
	cached := templates.NewCached(testFS)
	if err := cached.Preload(); err != nil {
		t.Fatal(err)
	}
	got, err := cached.Render("account/welcome", map[string]any{"Name": "Alice"})
	if err != nil || got != content {
		t.Fatalf("nested cached renderer mismatch: %+v / %v", got, err)
	}
}

func TestConsumerNotifyHubClientSmoke(t *testing.T) {
	client, err := notifyhub.New(notifyhub.Config{
		BaseURL:    "https://notifyhub.internal",
		ProjectKey: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestConsumerOutboxPayload(t *testing.T) {
	want := notificationsjob.Payload{
		Request: transport.Request{
			IdempotencyKey: "idem-key-consumer",
			Event:          "user.welcome",
			Email: &transport.EmailMessage{
				From:    "noreply@example.test",
				To:      []string{"alice@example.test"},
				Subject: "Welcome",
				Text:    "Hello Alice",
			},
		},
	}
	encoded, err := notificationsjob.MarshalPayload(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := notificationsjob.UnmarshalPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.Request.IdempotencyKey != want.Request.IdempotencyKey || got.Request.Event != want.Request.Event {
		t.Fatalf("payload contract changed: %#v", got)
	}
	if notificationsjob.JobName != "notifications_send" {
		t.Fatal("outbox job name changed")
	}

	job := notificationsjob.Must(notificationsjob.NewOptions(&dummyTransport{}))
	if err := job.Handle(context.Background(), encoded); err != nil {
		t.Fatalf("job handle failed: %v", err)
	}
}
