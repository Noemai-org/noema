package ledger

// Canary: the crash hook is a field of the type; declaring it and calling it is how the hook works. Neither is a violation.
type Point int

type Ledger struct {
	Boundary func(Point) error
}

func (l *Ledger) at(p Point) error {
	if l.Boundary == nil {
		return nil
	}
	return l.Boundary(p)
}
