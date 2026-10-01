package gonotifyjob_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/assurrussa/outbox/outbox"
	"github.com/assurrussa/outbox/shared/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	notificationsjob "github.com/assurrussa/gonotify/interfaces/outbox/notifications"
	"github.com/assurrussa/gonotify/transport"
)

type mockTransport struct {
	submitFunc func(ctx context.Context, req transport.Request) (transport.Receipt, error)
}

func (m *mockTransport) Submit(ctx context.Context, req transport.Request) (transport.Receipt, error) {
	if m.submitFunc != nil {
		return m.submitFunc(ctx, req)
	}
	return transport.Receipt{ID: "msg-1"}, nil
}

type fakeVersionedPutter struct {
	putFunc func(
		ctx context.Context,
		name string,
		schemaVersion outbox.SchemaVersion,
		payload string,
		availableAt time.Time,
	) (types.JobID, error)
}

func (f *fakeVersionedPutter) PutVersioned(
	ctx context.Context,
	name string,
	schemaVersion outbox.SchemaVersion,
	payload string,
	availableAt time.Time,
) (types.JobID, error) {
	if f.putFunc != nil {
		return f.putFunc(ctx, name, schemaVersion, payload, availableAt)
	}
	return types.JobID(uuid.New()), nil
}

func TestJob_MustInit(t *testing.T) {
	assert.Panics(t, func() {
		notificationsjob.Must(notificationsjob.NewOptions(nil))
	})
}

func TestJob_SchemaVersion(t *testing.T) {
	job := notificationsjob.Must(notificationsjob.NewOptions(&mockTransport{}))
	require.Equal(t, notificationsjob.JobName, job.Name())
	require.Equal(t, outbox.SchemaVersion(2), job.SchemaVersion())

	// Compile-time interface verification
	var _ outbox.VersionedJob = job
	var _ outbox.Job = job
}

func TestJob_Handle_Success(t *testing.T) {
	ctx := context.Background()
	var submittedReq transport.Request

	mockT := &mockTransport{
		submitFunc: func(_ context.Context, req transport.Request) (transport.Receipt, error) {
			submittedReq = req
			return transport.Receipt{ID: "receipt-123"}, nil
		},
	}

	job := notificationsjob.Must(notificationsjob.NewOptions(mockT))

	payload, err := notificationsjob.MarshalPayload(notificationsjob.Payload{
		Request: transport.Request{
			IdempotencyKey: "idem-key-1",
			Event:          "user.welcome",
			Email: &transport.EmailMessage{
				From:    "noreply@example.test",
				To:      []string{"alice@example.test"},
				Subject: "Welcome",
				Text:    "Hello Alice",
			},
		},
	})
	require.NoError(t, err)

	err = job.Handle(ctx, payload)
	require.NoError(t, err)
	assert.Equal(t, "idem-key-1", submittedReq.IdempotencyKey)
	assert.Equal(t, "user.welcome", submittedReq.Event)
	assert.Equal(t, "alice@example.test", submittedReq.Email.To[0])
}

func TestJob_Handle_PermanentOnPayloadErrors(t *testing.T) {
	ctx := context.Background()
	job := notificationsjob.Must(notificationsjob.NewOptions(&mockTransport{}))

	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "invalid json",
			payload: "invalid-json",
		},
		{
			name:    "empty json object",
			payload: "{}",
		},
		{
			name:    "missing idempotency key",
			payload: `{"request": {"event": "test", "email": {"from": "a", "to": ["b"]}}}`,
		},
		{
			name:    "idempotency key too short",
			payload: `{"request": {"idempotency_key": "short", "event": "test", "email": {"from": "a", "to": ["b"]}}}`,
		},
		{
			name: "idempotency key too long",
			payload: `{"request": {"idempotency_key": "` +
				strings.Repeat("a", 201) + `", "event": "test", "email": {"from": "a", "to": ["b"]}}}`,
		},
		{
			name:    "idempotency key with control chars",
			payload: `{"request": {"idempotency_key": "invalid\nkey", "event": "test", "email": {"from": "a", "to": ["b"]}}}`,
		},
		{
			name:    "idempotency key with leading whitespace",
			payload: `{"request": {"idempotency_key": "   valid-idempotency-key", "event": "test", "email": {"from": "a", "to": ["b"]}}}`,
		},
		{
			name:    "idempotency key with trailing whitespace",
			payload: `{"request": {"idempotency_key": "valid-idempotency-key   ", "event": "test", "email": {"from": "a", "to": ["b"]}}}`,
		},
		{
			name:    "missing message payload (neither email nor telegram)",
			payload: `{"request": {"idempotency_key": "valid-idempotency-key", "event": "test"}}`,
		},
		{
			name:    "legacy v1 payload without request",
			payload: `{"idempotency_key": "v1-key", "channel": "email"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := job.Handle(ctx, tt.payload)
			require.Error(t, err)
			assert.True(t, outbox.IsPermanent(err), "expected outbox.Permanent error, got: %v", err)
		})
	}
}

func TestJob_Handle_PermanentOnTerminalTransportErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		transportErr error
	}{
		{
			name:         "400 InvalidRequest",
			transportErr: transport.ErrInvalidRequest,
		},
		{
			name:         "409 IdempotencyConflict",
			transportErr: transport.ErrIdempotencyConflict,
		},
		{
			name:         "413 PayloadTooLarge",
			transportErr: transport.ErrPayloadTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockT := &mockTransport{
				submitFunc: func(_ context.Context, _ transport.Request) (transport.Receipt, error) {
					return transport.Receipt{}, tt.transportErr
				},
			}
			job := notificationsjob.Must(notificationsjob.NewOptions(mockT))

			payload, err := notificationsjob.MarshalPayload(notificationsjob.Payload{
				Request: transport.Request{
					IdempotencyKey: "idem-terminal",
					Event:          "user.terminal",
					Email:          &transport.EmailMessage{From: "a", To: []string{"b"}},
				},
			})
			require.NoError(t, err)

			err = job.Handle(ctx, payload)
			require.Error(t, err)
			assert.True(t, outbox.IsPermanent(err), "expected outbox.Permanent error, got %v", err)
		})
	}
}

func TestJob_Handle_QuotaErrorWithRetryAfter(t *testing.T) {
	ctx := context.Background()

	mockT := &mockTransport{
		submitFunc: func(_ context.Context, _ transport.Request) (transport.Receipt, error) {
			return transport.Receipt{}, &transport.QuotaError{
				Code:       "quota_exceeded",
				RetryAfter: 30 * time.Second,
			}
		},
	}
	job := notificationsjob.Must(notificationsjob.NewOptions(mockT))

	payload, err := notificationsjob.MarshalPayload(notificationsjob.Payload{
		Request: transport.Request{
			IdempotencyKey: "idem-quota",
			Event:          "user.quota",
			Email:          &transport.EmailMessage{From: "a", To: []string{"b"}},
		},
	})
	require.NoError(t, err)

	err = job.Handle(ctx, payload)
	require.Error(t, err)
	assert.False(t, outbox.IsPermanent(err), "quota error must not be permanent")

	retryAt, hasRetryAt := outbox.RetryTime(err)
	require.True(t, hasRetryAt, "expected RetryAt disposition on 429 with RetryAfter")
	expectedAt := time.Now().Add(30 * time.Second)
	assert.WithinDuration(t, expectedAt, retryAt, 2*time.Second)
}

func TestJob_Handle_UnauthorizedDeferDelay(t *testing.T) {
	ctx := context.Background()

	mockT := &mockTransport{
		submitFunc: func(_ context.Context, _ transport.Request) (transport.Receipt, error) {
			return transport.Receipt{}, transport.ErrUnauthorized
		},
	}
	job := notificationsjob.Must(notificationsjob.NewOptions(mockT))

	payload, err := notificationsjob.MarshalPayload(notificationsjob.Payload{
		Request: transport.Request{
			IdempotencyKey: "idem-unauth",
			Event:          "user.unauth",
			Email:          &transport.EmailMessage{From: "a", To: []string{"b"}},
		},
	})
	require.NoError(t, err)

	err = job.Handle(ctx, payload)
	require.Error(t, err)
	assert.False(t, outbox.IsPermanent(err), "unauthorized must not be permanent")

	deferAt, hasDeferAt := outbox.DeferTime(err)
	require.True(t, hasDeferAt, "expected DeferAt disposition on 401 Unauthorized without consuming attempts")
	expectedAt := time.Now().Add(5 * time.Minute)
	assert.WithinDuration(t, expectedAt, deferAt, 2*time.Second)
}

func TestJob_Handle_OrdinaryRetryOnTransientErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		transportErr error
	}{
		{
			name:         "503 TemporarilyUnavailable",
			transportErr: transport.ErrTemporarilyUnavailable,
		},
		{
			name:         "network error",
			transportErr: errors.New("connection reset by peer"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockT := &mockTransport{
				submitFunc: func(_ context.Context, _ transport.Request) (transport.Receipt, error) {
					return transport.Receipt{}, tt.transportErr
				},
			}
			job := notificationsjob.Must(notificationsjob.NewOptions(mockT))

			payload, err := notificationsjob.MarshalPayload(notificationsjob.Payload{
				Request: transport.Request{
					IdempotencyKey: "idem-transient",
					Event:          "user.transient",
					Email:          &transport.EmailMessage{From: "a", To: []string{"b"}},
				},
			})
			require.NoError(t, err)

			err = job.Handle(ctx, payload)
			require.Error(t, err)
			assert.False(t, outbox.IsPermanent(err), "transient error must not be permanent")
			_, hasRetryAt := outbox.RetryTime(err)
			assert.False(t, hasRetryAt, "transient error uses default outbox retry interval")
			assert.ErrorIs(t, err, tt.transportErr)
		})
	}
}

func TestPayload_Validation_FailEarly(t *testing.T) {
	const errWhitespace = "must not contain leading or trailing whitespace"

	tests := []struct {
		name    string
		req     transport.Request
		wantErr string
	}{
		{
			name: "missing idempotency key",
			req: transport.Request{
				Email: &transport.EmailMessage{From: "a", To: []string{"b"}},
			},
			wantErr: "missing idempotency_key",
		},
		{
			name: "idempotency key too short",
			req: transport.Request{
				IdempotencyKey: "short",
				Email:          &transport.EmailMessage{From: "a", To: []string{"b"}},
			},
			wantErr: "idempotency_key length must be between 8 and 200",
		},
		{
			name: "idempotency key too long",
			req: transport.Request{
				IdempotencyKey: strings.Repeat("x", 201),
				Email:          &transport.EmailMessage{From: "a", To: []string{"b"}},
			},
			wantErr: "idempotency_key length must be between 8 and 200",
		},
		{
			name: "idempotency key with carriage return",
			req: transport.Request{
				IdempotencyKey: "valid-key\r",
				Email:          &transport.EmailMessage{From: "a", To: []string{"b"}},
			},
			wantErr: "invalid control characters",
		},
		{
			name: "idempotency key with leading whitespace",
			req: transport.Request{
				IdempotencyKey: "   valid-key-123",
				Email:          &transport.EmailMessage{From: "a", To: []string{"b"}},
			},
			wantErr: errWhitespace,
		},
		{
			name: "idempotency key with trailing whitespace",
			req: transport.Request{
				IdempotencyKey: "valid-key-123   ",
				Email:          &transport.EmailMessage{From: "a", To: []string{"b"}},
			},
			wantErr: errWhitespace,
		},
		{
			name: "idempotency key with whitespace padding edge case",
			req: transport.Request{
				IdempotencyKey: strings.Repeat(" ", 100) + strings.Repeat("a", 150),
				Email:          &transport.EmailMessage{From: "a", To: []string{"b"}},
			},
			wantErr: errWhitespace,
		},
		{
			name: "missing email and telegram",
			req: transport.Request{
				IdempotencyKey: "valid-key-123",
			},
			wantErr: "missing message payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := notificationsjob.ValidateRequest(tt.req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)

			_, marshalErr := notificationsjob.MarshalPayload(notificationsjob.Payload{Request: tt.req})
			require.Error(t, marshalErr)
			assert.Contains(t, marshalErr.Error(), tt.wantErr)
		})
	}
}

func TestPut_Helper(t *testing.T) {
	ctx := context.Background()
	req := transport.Request{
		IdempotencyKey: "idem-put-helper-1",
		Event:          "user.registered",
		Email:          &transport.EmailMessage{From: "noreply@test", To: []string{"u@test"}},
	}
	expectedID := types.JobID(uuid.New())

	var capturedName string
	var capturedVersion outbox.SchemaVersion
	var capturedPayload string
	var capturedTime time.Time

	putter := &fakeVersionedPutter{
		putFunc: func(
			_ context.Context,
			name string,
			schemaVersion outbox.SchemaVersion,
			payload string,
			availableAt time.Time,
		) (types.JobID, error) {
			capturedName = name
			capturedVersion = schemaVersion
			capturedPayload = payload
			capturedTime = availableAt
			return expectedID, nil
		},
	}

	now := time.Now()
	jobID, err := notificationsjob.Put(ctx, putter, req, now)
	require.NoError(t, err)
	assert.Equal(t, expectedID, jobID)
	assert.Equal(t, notificationsjob.JobName, capturedName)
	assert.Equal(t, notificationsjob.SchemaVersion, capturedVersion)
	assert.Equal(t, now, capturedTime)

	decoded, err := notificationsjob.UnmarshalPayload(capturedPayload)
	require.NoError(t, err)
	assert.Equal(t, req.IdempotencyKey, decoded.Request.IdempotencyKey)

	// Fails early on invalid request
	_, err = notificationsjob.Put(ctx, putter, transport.Request{IdempotencyKey: "short"}, now)
	require.Error(t, err)

	// Fails on nil putter
	_, err = notificationsjob.Put(ctx, nil, req, now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outbox putter is nil")
}
