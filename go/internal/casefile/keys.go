package casefile

// Slot names the shared case file (artifact keys).
type Slot string

const (
	SlotSignals    Slot = "signals"
	SlotSentiment  Slot = "sentiment"
	SlotDiagnosis  Slot = "diagnosis"
	SlotStrategy   Slot = "strategy"
	SlotLegal      Slot = "legal"
	SlotApproval   Slot = "approval"
	SlotAction     Slot = "action"
	SlotFeedback   Slot = "feedback"
	SlotStatus     Slot = "status"
	SlotPostMortem Slot = "post_mortem"
)
