package signingtest

import (
	"context"
	"testing"
	"time"
)

func contextTimeout(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}
