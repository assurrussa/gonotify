package gonotifyjob_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/assurrussa/outbox/outbox"
	"github.com/stretchr/testify/require"

	notificationsjob "github.com/assurrussa/gonotify/interfaces/outbox/notifications"
	"github.com/assurrussa/gonotify/transport"
)

func TestOptions_TransportValidation(t *testing.T) {
	var typedNil *mockTransport
	for _, tr := range []transport.Transport{nil, typedNil} {
		_, err := notificationsjob.New(notificationsjob.NewOptions(tr))
		require.Error(t, err)
	}
	var nilOptions *notificationsjob.Options
	require.Error(t, nilOptions.Validate())

	setterCalled := false
	opts := notificationsjob.NewOptions(&mockTransport{}, func(_ *notificationsjob.Options) {
		setterCalled = true
	})
	_, err := notificationsjob.New(opts)
	require.NoError(t, err)
	require.True(t, setterCalled)
}

func TestPut_RejectsTypedNilPutter(t *testing.T) {
	var putter *fakeVersionedPutter
	_, err := notificationsjob.Put(context.Background(), putter, transport.Request{
		IdempotencyKey: "nil-putter-test",
		Telegram:       &transport.TelegramMessage{ChatID: "123", Text: "message"},
	}, time.Now())
	require.Error(t, err)
}

func TestMarshalPayload_RejectsKeyThatJSONWouldChange(t *testing.T) {
	_, err := notificationsjob.MarshalPayload(notificationsjob.Payload{Request: transport.Request{
		IdempotencyKey: "valid-key-\xff",
		Telegram:       &transport.TelegramMessage{ChatID: "123", Text: "message"},
	}})
	require.ErrorIs(t, err, transport.ErrInvalidRequest)
}

func expirationPayload(t *testing.T, expiresAt *time.Time) string {
	t.Helper()
	payload, err := notificationsjob.MarshalPayload(notificationsjob.Payload{Request: transport.Request{
		IdempotencyKey: "expiration-test",
		ExpiresAt:      expiresAt,
		Telegram:       &transport.TelegramMessage{ChatID: "123", Text: "OTP"},
	}})
	require.NoError(t, err)
	return payload
}

func TestJob_ExpirationBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, offset := range []time.Duration{-time.Second, 0, time.Second} {
			expiresAt := time.Now().Add(offset)
			calls := 0
			job := notificationsjob.Must(notificationsjob.NewOptions(&mockTransport{
				submitFunc: func(_ context.Context, _ transport.Request) (transport.Receipt, error) {
					calls++
					return transport.Receipt{ID: "receipt"}, nil
				},
			}))
			require.NoError(t, job.Handle(context.Background(), expirationPayload(t, &expiresAt)))
			if offset > 0 {
				require.Equal(t, 1, calls)
			} else {
				require.Zero(t, calls, "expired jobs must complete without submission")
			}
		}
	})
}

func TestJob_RetryStopsAtExpiration(t *testing.T) {
	for _, transportErr := range []error{
		transport.ErrUnauthorized,
		&transport.QuotaError{RetryAfter: time.Hour},
	} {
		t.Run(transportErr.Error(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				expiresAt := time.Now().Add(time.Minute)
				calls := 0
				job := notificationsjob.Must(notificationsjob.NewOptions(&mockTransport{
					submitFunc: func(_ context.Context, _ transport.Request) (transport.Receipt, error) {
						calls++
						return transport.Receipt{}, transportErr
					},
				}))
				payload := expirationPayload(t, &expiresAt)
				err := job.Handle(context.Background(), payload)
				require.ErrorIs(t, err, transportErr)
				at, ok := outbox.DeferTime(err)
				if errors.Is(transportErr, transport.ErrQuotaExceeded) {
					at, ok = outbox.RetryTime(err)
				}
				require.True(t, ok)
				require.True(t, expiresAt.Equal(at), "next attempt must be capped at expiration")
				time.Sleep(time.Minute)
				require.NoError(t, job.Handle(context.Background(), payload))
				require.Equal(t, 1, calls, "expired retry must not submit again")
			})
		})
	}
}

func TestJob_ExpirationDuringSubmission(t *testing.T) {
	for _, transportErr := range []error{
		transport.ErrUnauthorized,
		transport.ErrInvalidRequest,
		errors.New("network failure"),
	} {
		t.Run(transportErr.Error(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				expiresAt := time.Now().Add(time.Second)
				job := notificationsjob.Must(notificationsjob.NewOptions(&mockTransport{
					submitFunc: func(_ context.Context, _ transport.Request) (transport.Receipt, error) {
						time.Sleep(time.Second)
						return transport.Receipt{}, transportErr
					},
				}))
				require.NoError(t, job.Handle(context.Background(), expirationPayload(t, &expiresAt)))
			})
		})
	}
}

func TestJob_TransportExpirationCompletes(t *testing.T) {
	job := notificationsjob.Must(notificationsjob.NewOptions(&mockTransport{
		submitFunc: func(_ context.Context, _ transport.Request) (transport.Receipt, error) {
			return transport.Receipt{}, fmt.Errorf("gateway: %w", transport.ErrExpired)
		},
	}))
	require.NoError(t, job.Handle(context.Background(), expirationPayload(t, nil)))
}
