package gonotifyjob

import (
	"errors"
	"reflect"

	"github.com/assurrussa/gonotify/transport"
)

// Options configures the notification outbox job.
type Options struct {
	transport transport.Transport
}

// OptOptionsSetter customizes job options. Kept for constructor compatibility.
type OptOptionsSetter func(o *Options)

// NewOptions configures a job with its required transport.
func NewOptions(t transport.Transport, options ...OptOptionsSetter) Options {
	o := Options{transport: t}
	for _, opt := range options {
		opt(&o)
	}
	return o
}

// Validate checks that a usable transport was supplied, including typed nils.
func (o *Options) Validate() error {
	if o == nil || isNil(o.transport) {
		return errors.New("transport is required")
	}
	return nil
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
