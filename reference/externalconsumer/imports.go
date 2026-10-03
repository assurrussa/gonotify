package externalconsumer

import (
	// Compile-check the stable runtime package; consumer_test checks its validator and expiration error.
	_ "github.com/assurrussa/gonotify"
	// Compile-check the stable host support package.
	_ "github.com/assurrussa/gonotify/di"
	// Compile-check the stable outbox support package.
	_ "github.com/assurrussa/gonotify/interfaces/outbox/notifications"
	// Compile-check templates and transports, including dedicated confidential email.
	_ "github.com/assurrussa/gonotify/templates"
	_ "github.com/assurrussa/gonotify/transport"
	_ "github.com/assurrussa/gonotify/transport/notifyhub"
)
