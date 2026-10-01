package main

import (
	"context"
	"log"
	"os"
	"testing/fstest"
	"time"

	notificationsjob "github.com/assurrussa/gonotify/interfaces/outbox/notifications"
	"github.com/assurrussa/gonotify/templates"
	"github.com/assurrussa/gonotify/transport"
	"github.com/assurrussa/gonotify/transport/notifyhub"
)

func main() {
	ctx := context.Background()

	// 1. Immutable template rendering with layout and auto-escaping.
	// In production, pass os.DirFS("templates/email") or //go:embed.
	mockFS := fstest.MapFS{
		"welcome/html.tmpl":    &fstest.MapFile{Data: []byte("<h1>Welcome, {{ .UserName }}!</h1>")},
		"welcome/txt.tmpl":     &fstest.MapFile{Data: []byte("Welcome, {{ .UserName }}!")},
		"welcome/subject.tmpl": &fstest.MapFile{Data: []byte("Welcome to the Platform")},
	}

	renderer := templates.NewCached(mockFS)
	if err := renderer.Preload(); err != nil {
		log.Fatalf("failed to preload templates: %v", err)
	}

	rendered, err := renderer.Render("welcome", map[string]any{
		"UserName": "Alice",
	})
	if err != nil {
		log.Fatalf("failed to render template: %v", err)
	}

	// 2. Prepare the delivery request with a stable idempotency key.
	req := transport.Request{
		IdempotencyKey: "signup-alice-20261001",
		Event:          "user.welcome",
		Email: &transport.EmailMessage{
			From:    "noreply@example.com",
			To:      []string{"alice@example.com"},
			Subject: rendered.Subject,
			HTML:    rendered.HTML,
			Text:    rendered.Text,
		},
	}

	// 3a. Direct delivery via NotifyHub gateway client.
	hubURL := os.Getenv("NOTIFYHUB_URL")
	hubKey := os.Getenv("NOTIFYHUB_PROJECT_KEY")
	if hubURL != "" && hubKey != "" {
		client, err := notifyhub.New(notifyhub.Config{
			BaseURL:    hubURL,
			ProjectKey: hubKey,
			Timeout:    5 * time.Second,
		})
		if err != nil {
			log.Fatalf("failed to initialize notifyhub client: %v", err)
		}

		receipt, err := client.Submit(ctx, req)
		if err != nil {
			log.Fatalf("delivery failed: %v", err)
		}
		log.Printf("submitted notification: id=%s duplicate=%t deliveries=%d\n",
			receipt.ID, receipt.Duplicate, len(receipt.Deliveries))
	}

	// 3b. Or serialize to outbox payload for transactional deferred delivery.
	// In production with an outbox service, use:
	//   jobID, err := notificationsjob.Put(ctx, outboxService, req, time.Now())
	// Or manually marshal the payload:
	outboxPayload, err := notificationsjob.MarshalPayload(notificationsjob.Payload{
		Request: req,
	})
	if err != nil {
		log.Fatalf("failed to marshal outbox payload: %v", err)
	}
	log.Printf("ready to enqueue to outbox (%s schema v%d): %s\n",
		notificationsjob.JobName, notificationsjob.SchemaVersion, outboxPayload)
}
