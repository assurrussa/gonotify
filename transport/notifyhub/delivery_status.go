package notifyhub

// DeliveryOutcome separates queue ownership from provider handoff and inbox delivery.
// Always retain the receipt ID and original status for reconciliation.
type DeliveryOutcome string

const (
	DeliveryPending          DeliveryOutcome = "pending"
	DeliveryProviderAccepted DeliveryOutcome = "provider_accepted"
	DeliveryDelivered        DeliveryOutcome = "delivered"
	DeliverySimulated        DeliveryOutcome = "simulated"
	DeliveryFailed           DeliveryOutcome = "failed"
	DeliveryUnknown          DeliveryOutcome = "unknown"
)

// ClassifyDeliveryStatus classifies one delivery, not an aggregate receipt.
// Queue/retry statuses never mean provider acceptance. Unknown and future statuses
// cannot establish delivery or non-delivery: reconcile the same receipt using Get,
// without issuing a fresh key or falling back to another transport. A duplicate
// receipt preserves its actual status and does not itself imply successful delivery.
// Failed is terminal gateway state, not proof that a provider never accepted mail.
// Bounce/complaint snapshots carry no prior-handoff evidence in the current API,
// so they conservatively classify as unknown rather than rejected or accepted.
func ClassifyDeliveryStatus(status string) DeliveryOutcome {
	switch DeliveryStatus(status) {
	case StatusQueued, StatusSending, StatusRetry:
		return DeliveryPending
	case StatusAccepted:
		return DeliveryProviderAccepted
	case StatusDelivered:
		return DeliveryDelivered
	case StatusSimulated:
		return DeliverySimulated
	case StatusFailed, StatusExpired, StatusSuppressed:
		return DeliveryFailed
	default:
		return DeliveryUnknown
	}
}
