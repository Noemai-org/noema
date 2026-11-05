package verifier

import (
	"errors"
	"fmt"
)

// The reads of one verification (OUT-01): each page on the retry budget, every attempt kept for the Record. Split from
// verifier.go, which decides; this file only reads.

// readAll reads every page of a query, each page on the retry budget. It returns the cases of a complete answer, or the
// reason there is none. An answer that is not complete is never read as "no such case" (INV-19).
func (r *run) readAll(q CaseQuery, kind string) ([]Case, Reason) {
	var all []Case
	for page := 1; page <= r.v.cfg.Budget.MaxPages; page++ {
		pg, reason := r.readPage(q, kind, page)
		if reason != "" {
			return nil, reason
		}
		all = append(all, pg.Items...)
		if pg.Complete {
			return all, ""
		}
		if pg.NextCursor == "" {
			return nil, ReasonIncompleteRead
		}
		q.Cursor = pg.NextCursor
	}
	return nil, ReasonIncompleteRead
}

// readPage reads one page. An unavailable source is retried after each backoff of the budget; a malformed or refused
// answer is final. Every attempt is kept as a VERIFICATION_READ record; the successful reads are the verdict's evidence.
func (r *run) readPage(q CaseQuery, kind string, page int) (CasePage, Reason) {
	for attempt := 0; ; attempt++ {
		pg, err := r.v.cfg.Cases.FindCases(r.ctx, q)
		rec := ReadRecord{EvidenceID: r.newID(idEvidence), Query: kind, Attempt: attempt + 1, Page: page, At: r.v.cfg.Clock.Now()}
		var re *ReadError
		switch {
		case err == nil:
			rec.Status, rec.Items = readOK, len(pg.Items)
			r.reads, r.evidence = append(r.reads, rec), append(r.evidence, rec.EvidenceID)
			return pg, ""
		case errors.As(err, &re) && re.Kind == ReadMalformed:
			rec.Status = readMalformed
			r.reads = append(r.reads, rec)
			return CasePage{}, ReasonMalformedRead
		case errors.As(err, &re) && re.Kind == ReadRefused:
			rec.Status = readRefused
			r.reads = append(r.reads, rec)
			return CasePage{}, ReasonReadRefused
		}
		rec.Status = readUnavailable // an error the reader did not classify is an unavailable source, never a result
		r.reads = append(r.reads, rec)
		if attempt >= len(r.v.cfg.Budget.Backoffs) {
			return CasePage{}, ReasonReadsExhausted
		}
		if err := r.v.cfg.Waiter.Wait(r.ctx, r.v.cfg.Budget.Backoffs[attempt]); err != nil {
			r.err = fmt.Errorf("verifier: wait before a read retry: %w", err)
			return CasePage{}, ReasonReadsExhausted
		}
	}
}
