package controller_test

// REQ-ENV-001, REQ-EXE-007: adapters report a transport class in their own type because they must not import
// execution. This test keeps the two lists identical, so a class added on one side and not the other fails
// here and not in production.

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/execution/controller"
)

func TestAdapterTransportClassesMatchTheController(t *testing.T) {
	pairs := []struct {
		adapter adapters.TransportClass
		ctl     controller.TransportClass
	}{
		{adapters.ClassAck2xx, controller.TransportAck2xx},
		{adapters.ClassVersionConflict, controller.TransportVersionConflict},
		{adapters.ClassDuplicate409, controller.TransportDuplicate409},
		{adapters.ClassRejected4xx, controller.TransportRejected4xx},
		{adapters.ClassTimeout, controller.TransportTimeout},
		{adapters.ClassConnectionReset, controller.TransportConnectionReset},
		{adapters.ClassServer5xx, controller.TransportServer5xx},
		{adapters.ClassNoResponse, controller.TransportNoResponse},
		{adapters.ClassDefect, controller.TransportDefect},
	}
	for _, p := range pairs {
		if string(p.adapter) != string(p.ctl) {
			t.Errorf("adapter class %q and controller class %q differ", p.adapter, p.ctl)
		}
	}
}
