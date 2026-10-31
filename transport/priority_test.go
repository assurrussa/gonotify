package transport_test

import (
	"errors"
	"testing"
	"time"

	"github.com/assurrussa/gonotify/transport"
)

func TestValidatePriority(t *testing.T) {
	future := time.Now().Add(time.Hour)
	zero := time.Time{}
	for _, tc := range []struct {
		priority transport.Priority
		expiry   *time.Time
		valid    bool
	}{
		{"", nil, true},
		{transport.PriorityNormal, nil, true},
		{transport.PriorityHigh, &future, true},
		{transport.PriorityHigh, nil, false},
		{transport.PriorityHigh, &zero, false},
		{"low", &future, false},
		{" high ", &future, false},
	} {
		err := transport.ValidatePriority(tc.priority, tc.expiry)
		if (err == nil) != tc.valid || (err != nil && !errors.Is(err, transport.ErrInvalidRequest)) {
			t.Errorf("%q: %v", tc.priority, err)
		}
	}
}
