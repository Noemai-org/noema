package proxy

import (
	"strconv"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/jsonx"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/plan"
)

func itoa(n int) string { return strconv.Itoa(n) }

// faultTree applies the faults that change the JSON document: drift, skew, injected text and event-feed faults.
// detail receives a small, deterministic account of what happened, for the trace.
func (s *Server) faultTree(c *call, tree any, detail map[string]any) any {
	e := c.eff
	if c.rt.EventFeed != nil {
		tree = s.eventFeed(c, tree, detail)
	}
	if e == nil {
		return tree
	}
	switch e.Name {
	case plan.SchemaDrift:
		var field string
		tree, field = jsonx.Drift(tree, e.Change, e.Field)
		detail["field"], detail["applied"] = field, field != ""
	case plan.ClockSkew:
		var n int
		tree, n = jsonx.Skew(tree, e.Skew)
		detail["shifted"] = n
	case plan.InjectionPayload:
		var field string
		tree, field = jsonx.Inject(tree, e.Field, int(e.Pick%1024))
		detail["field"], detail["applied"] = field, field != ""
	}
	return tree
}

// eventFeed applies the event-delivery faults to the events of this response, then adds any withheld events that
// are now due, at the end: a late event arrives after the events that followed it.
func (s *Server) eventFeed(c *call, tree any, detail map[string]any) any {
	key := c.rt.Target + "/" + c.rt.Operation
	events, ok := jsonx.EventArray(tree, c.rt.EventFeed.Events)
	if !ok {
		if c.eff != nil {
			detail["applied"] = false
		}
		return tree
	}
	if e := c.eff; e != nil {
		applied, known := len(events) > 0, true
		switch e.Name {
		case plan.DropWebhook:
			if applied {
				events = events[1:]
			}
		case plan.DuplicateWebhook:
			if applied {
				events = append([]any{events[0]}, events...)
			}
		case plan.ReorderEvents:
			applied = len(events) >= 2
			if applied {
				i := int(e.Pick % uint64(len(events)-1))
				events = append([]any(nil), events...)
				events[i], events[i+1] = events[i+1], events[i]
				detail["swapped"] = i
			}
		case plan.LateEvent:
			if applied {
				s.held.hold(key, c.now.Add(e.Delay), events)
				detail["held"] = len(events)
				events = []any{}
			}
		default:
			known = false // an effect for the document as a whole, applied by faultTree
		}
		if known {
			detail["applied"] = applied
		}
	}
	if due := s.held.due(key, c.now); len(due) > 0 {
		events = append(append([]any(nil), events...), due...)
		detail["released"] = len(due)
	}
	if events == nil {
		events = []any{}
	}
	return jsonx.SetEventArray(tree, c.rt.EventFeed.Events, events)
}
