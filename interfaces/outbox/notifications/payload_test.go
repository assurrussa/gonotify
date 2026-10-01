package gonotifyjob_test

import (
	"testing"

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
