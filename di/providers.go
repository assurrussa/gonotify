package di

import (
	gonotifyjob "github.com/assurrussa/gonotify/interfaces/outbox/notifications"
	"github.com/assurrussa/gonotify/transport"
	"github.com/assurrussa/gonotify/transport/notifyhub"
)

// ProvideNotifyHubClient wires a NotifyHub transport client from configuration.
func ProvideNotifyHubClient(cfg notifyhub.Config) (transport.Transport, error) {
	return notifyhub.New(cfg)
}

// ProvideOutboxJob wires a notification outbox job backed by a transport.
func ProvideOutboxJob(t transport.Transport) (*gonotifyjob.Job, error) {
	return gonotifyjob.New(gonotifyjob.NewOptions(t))
}
