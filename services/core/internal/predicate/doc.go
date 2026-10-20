// Package predicate: The predicate language of SoT §43.7: a parser and an evaluator, pure, shared by
// reconciliation rules, Action Graph pre- and postconditions and outcome definitions. Type errors are
// caught at pack load, not at run time.
//
// Implements: REQ-VER-002, REQ-RES-001, REQ-AG-001.
//
// May import: the Go standard library only (no I/O package).
//
// Must never: import trust, execution, outcome or record; perform I/O; read the clock or a random source;
// print (the predicate-purity lint).
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package predicate
