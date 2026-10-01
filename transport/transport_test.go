package transport_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/assurrussa/gonotify/transport"
)

func TestValidateIdempotencyKey(t *testing.T) {
	const (
		errControlChars = "invalid control characters"
		errWhitespace   = "must not contain leading or trailing whitespace"
		errLength       = "idempotency_key length must be between 8 and 200"
	)

	tests := []struct {
		name    string
		key     string
		wantErr string
	}{
		{
			name: "valid key",
			key:  "valid-idempotency-key",
		},
		{
			name: "valid minimum length",
			key:  "12345678",
		},
		{
			name:    "invalid UTF-8 key",
			key:     "valid-key-\xff",
			wantErr: "idempotency_key must be valid UTF-8",
		},
		{
			name: "valid maximum length",
			key:  strings.Repeat("k", 200),
		},
		{
			name: "valid UTF-8 byte boundary",
			key:  strings.Repeat("я", 100),
		},
		{
			name:    "UTF-8 over byte boundary",
			key:     strings.Repeat("я", 101),
			wantErr: errLength,
		},
		{
			name:    "empty key",
			key:     "",
			wantErr: "missing idempotency_key",
		},
		{
			name:    "key too short (<8)",
			key:     "short",
			wantErr: errLength,
		},
		{
			name:    "key too long (>200)",
			key:     strings.Repeat("k", 201),
			wantErr: errLength,
		},
		{
			name:    "key with carriage return",
			key:     "valid-key\r",
			wantErr: errControlChars,
		},
		{
			name:    "key with newline",
			key:     "valid-key\n",
			wantErr: errControlChars,
		},
		{
			name:    "key with null byte",
			key:     "valid-key\x00",
			wantErr: errControlChars,
		},
		{
			name:    "key with leading whitespace",
			key:     "  valid-key-123",
			wantErr: errWhitespace,
		},
		{
			name:    "key with trailing whitespace",
			key:     "valid-key-123  ",
			wantErr: errWhitespace,
		},
		{
			name:    "key with whitespace padding edge case",
			key:     strings.Repeat(" ", 100) + strings.Repeat("a", 150),
			wantErr: errWhitespace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := transport.ValidateIdempotencyKey(tt.key)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.ErrorIs(t, err, transport.ErrInvalidRequest)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateIdempotencyKey_AllASCIIControls(t *testing.T) {
	for b := byte(0); b <= 0x7f; b++ {
		if b >= 0x20 && b != 0x7f {
			continue
		}
		key := "valid-" + string([]byte{b}) + "-key"
		require.ErrorIs(t, transport.ValidateIdempotencyKey(key), transport.ErrInvalidRequest, "control byte %d", b)
	}
}
