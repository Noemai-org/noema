package decide

// SlotStatus is the status of one intent slot (the Intent Object contract).
type SlotStatus string

// The four slot statuses.
const (
	SlotResolved   SlotStatus = "RESOLVED"
	SlotUnresolved SlotStatus = "UNRESOLVED"
	SlotAmbiguous  SlotStatus = "AMBIGUOUS"
	SlotAbsent     SlotStatus = "ABSENT"
)

// IntentView is what Admit reads from an admitted Intent Object: its version, the material ambiguity flags it carries and
// the status of every required slot.
type IntentView struct {
	Version       int
	MaterialFlags []string
	RequiredSlots map[string]SlotStatus
}

// ResolvedIntent is an intent with no material ambiguity and every required slot RESOLVED. Only Admit makes one.
type ResolvedIntent struct {
	version int
	ok      bool
}

// UnresolvedIntent is any other intent. It exposes only Clarify and Escalate. Only Admit makes one.
type UnresolvedIntent struct {
	version int
	ok      bool
}

// Admission is the result of Admit: exactly one of the two.
type Admission struct {
	resolved   ResolvedIntent
	unresolved UnresolvedIntent
}

// Admit sorts an intent into the resolved or the unresolved type (REQ-AMB-001, INV-06). A required slot that is not
// RESOLVED, an absent required slot, or any material flag makes it unresolved.
func Admit(v IntentView) Admission {
	resolved := len(v.MaterialFlags) == 0
	for _, s := range v.RequiredSlots {
		resolved = resolved && s == SlotResolved
	}
	if resolved {
		return Admission{resolved: ResolvedIntent{version: v.Version, ok: true}}
	}
	return Admission{unresolved: UnresolvedIntent{version: v.Version, ok: true}}
}

// Resolved returns the resolved intent when there is one.
func (a Admission) Resolved() (ResolvedIntent, bool) { return a.resolved, a.resolved.ok }

// Unresolved returns the unresolved intent when there is one.
func (a Admission) Unresolved() (UnresolvedIntent, bool) { return a.unresolved, a.unresolved.ok }

// Version is the Intent Object version the intent was admitted at.
func (r ResolvedIntent) Version() int { return r.version }

// Version is the Intent Object version the intent was admitted at.
func (u UnresolvedIntent) Version() int { return u.version }
