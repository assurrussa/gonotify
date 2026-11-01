package gonotifyjob_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	notificationsjob "github.com/assurrussa/gonotify/interfaces/outbox/notifications"
	"github.com/assurrussa/gonotify/transport"
)

func TestMarshalUnmarshal_Transport(t *testing.T) {
	msg := notificationsjob.Payload{
		Request: transport.Request{
			IdempotencyKey: "idem-outbox-key",
			Event:          "user.signup",
			Email: &transport.EmailMessage{
				From:    "noreply@example.test",
				To:      []string{"alice@example.test"},
				Subject: "Welcome",
				HTML:    "<p>Hello!</p>",
				Text:    "Hello!",
			},
			Telegram: &transport.TelegramMessage{
				ChatID: "998877",
				Text:   "New signup",
			},
		},
	}

	v, err := notificationsjob.MarshalPayload(msg)
	require.NoError(t, err)

	msg2, err := notificationsjob.UnmarshalPayload(v)
	require.NoError(t, err)
	assert.Equal(t, msg.Request.IdempotencyKey, msg2.Request.IdempotencyKey)
	assert.Equal(t, msg.Request.Event, msg2.Request.Event)
	assert.Equal(t, msg.Request.Email.From, msg2.Request.Email.From)
	assert.Equal(t, msg.Request.Telegram.ChatID, msg2.Request.Telegram.ChatID)
}

func TestUnmarshal_InvalidJSON(t *testing.T) {
	_, err := notificationsjob.UnmarshalPayload("not json")
	require.Error(t, err)
}

func TestPriorityAndExpirySurviveOutboxRoundTrip(t *testing.T) {
	expires := time.Now().Add(time.Hour).Truncate(time.Second).Add(123 * time.Millisecond)
	request := transport.Request{
		IdempotencyKey: "stable-outbox-key", Event: "auth.login",
		Priority: transport.PriorityHigh, ExpiresAt: &expires,
		Email: &transport.EmailMessage{Text: "synthetic-rendered-content"},
	}
	encoded, err := notificationsjob.MarshalPayload(notificationsjob.Payload{Request: request})
	require.NoError(t, err)
	got, err := notificationsjob.UnmarshalPayload(encoded)
	require.NoError(t, err)
	assert.Equal(t, request.Priority, got.Request.Priority)
	require.NotNil(t, got.Request.ExpiresAt)
	assert.True(t, expires.Equal(*got.Request.ExpiresAt))
	assert.Equal(t, request.IdempotencyKey, got.Request.IdempotencyKey)
	assert.Equal(t, request.Email.Text, got.Request.Email.Text)
	request.Priority = "low"
	_, err = notificationsjob.MarshalPayload(notificationsjob.Payload{Request: request})
	require.ErrorIs(t, err, transport.ErrInvalidRequest)
	request.Priority = transport.PriorityHigh
	request.ExpiresAt = nil
	_, err = notificationsjob.MarshalPayload(notificationsjob.Payload{Request: request})
	require.ErrorIs(t, err, transport.ErrInvalidRequest)
}

func TestLegacyDefaultPriorityDoesNotChangeOutboxPayload(t *testing.T) {
	request := transport.Request{
		IdempotencyKey: "legacy-empty-priority",
		Email:          &transport.EmailMessage{Text: "legacy-body"},
	}
	encoded, err := notificationsjob.MarshalPayload(notificationsjob.Payload{Request: request})
	require.NoError(t, err)
	require.NotContains(t, encoded, `"priority"`)
	decoded, err := notificationsjob.UnmarshalPayload(encoded)
	require.NoError(t, err)
	require.Empty(t, decoded.Request.Priority)
	require.Equal(t, request.IdempotencyKey, decoded.Request.IdempotencyKey)
}
