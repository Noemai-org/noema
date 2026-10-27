// Package migrations embeds the forward-only SQL migrations (AGENTS.md section 7, PERD 38, REQ-DATA-001).
// A migration is never edited after merge; a change is a new file with the next number. platform/db applies them.
package migrations

import "embed"

// FS holds every NNNN_name.sql file in this directory, in file-name order.
//
//go:embed *.sql
var FS embed.FS
