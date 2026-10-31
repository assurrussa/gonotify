package externalconsumer

// RuntimePackages are stable packages used to create and send notifications.
// Their contract includes idempotency validation and ErrExpired for normal expiration.
// The transport package includes normal/high priorities and validation. NotifyHub
// exposes queue status classification and deprecated synchronous confidential-email outcomes.
var RuntimePackages = [...]string{
	"github.com/assurrussa/gonotify",
	"github.com/assurrussa/gonotify/templates",
	"github.com/assurrussa/gonotify/transport",
	"github.com/assurrussa/gonotify/transport/notifyhub",
}

// HostSupportPackages are stable packages used by host applications for process
// wiring and dependency injection.
var HostSupportPackages = [...]string{
	"github.com/assurrussa/gonotify/di",
}

// OutboxSupportPackages are stable packages used to process notification outbox
// jobs.
var OutboxSupportPackages = [...]string{
	"github.com/assurrussa/gonotify/interfaces/outbox/notifications",
}

var SupportedPackages = joinPackageGroups(
	RuntimePackages[:],
	HostSupportPackages[:],
	OutboxSupportPackages[:],
)

var SupportedPackageCount = len(SupportedPackages)

func joinPackageGroups(groups ...[]string) []string {
	var count int
	for _, group := range groups {
		count += len(group)
	}

	packages := make([]string, 0, count)
	for _, group := range groups {
		packages = append(packages, group...)
	}

	return packages
}
