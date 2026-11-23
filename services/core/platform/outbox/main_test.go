package outbox_test

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

// The tests that need a database start the throwaway server of internal/testpg on first use; the unit tests of this package start none.
func TestMain(m *testing.M) { testpg.Main(m) }
