package main

// SEC-09 (REQ-SEC-005, INV-03, INV-25): the drill, on a simulated clock. A grant is issued every tick for the length of the run while the key ring goes from one key, to two (the highest
// version signs, either verifies), to the new key alone. At every tick every grant still inside its validity window is verified against the ring in force, and a corpus of forgeries is
// presented. The ring in force is whatever the caller gives (the command gives rings loaded from key files, as the core loads them). Everything here is SYNTHETIC; nothing is printed but counts.

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

const (
	tick     = 30 * time.Second
	validity = 10 * time.Minute
	length   = 90 * time.Minute
	overlap  = 30 * time.Minute // the second key joins here

	minChecks    = 1000 // fewer checks than this is not a drill
	minForgeries = 500

	oldID = "grant.hmac.1"
	newID = "grant.hmac.2"
)

var epoch = time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC)

// ringAt is the ring in force at a simulated offset.
type ringAt func(at time.Duration) (*grants.Keys, error)

// plan says what the run needs besides the ring: when the old key is retired, and the old and an attacker's secret (to forge with a retired or a made-up key).
type plan struct {
	ring      ringAt
	retireAt  time.Duration
	oldSecret []byte
	attacker  []byte
}

// result holds counts only.
type result struct {
	issued, signedOld, signedNew int
	checks, rejectedValid        int
	tried, forgedAccepted        int
}

type issued struct {
	g       contracts.DecisionGrantV1
	at      time.Duration
	expires time.Duration
}

func allowAt(at time.Duration) (decide.Decision, error) {
	r, _ := decide.Admit(decide.IntentView{Version: 2}).Resolved()
	d, err := r.Allow(decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass})
	if err != nil {
		return d, err
	}
	evaluated := epoch.Add(at).Unix()
	return d.WithBasis(decide.Basis{PackVersion: "fin.pack@1.0.0", PolicyRules: []decide.PolicyRuleVersion{{RuleID: "fin.pol.default.permit_case_writes", PolicyID: "fin.pol", Version: "3.1"}},
		EvidenceSnapshotID: "snap_01", EvidenceFreshness: []decide.Freshness{{EvidenceID: "ev_1", EffectiveAt: evaluated - 30, Source: "fin.src.ledger", AgeSeconds: 30, EffectiveLimitSeconds: 300, LimitSetBy: decide.LimitBySource}}, EvaluatedAt: evaluated})
}

func sum(c string) contracts.CommonV1Hash {
	return contracts.CommonV1Hash("sha256:" + strings.Repeat(c, 64))
}

func step() (grants.Step, error) {
	permit, err := reconcile.NewFreezeGate(nil).PermitTarget(false, "fin.cap.create_escalation_case", "fin.transaction:TX88213")
	if err != nil {
		return grants.Step{}, err
	}
	return grants.Step{Permission: permit, TenantID: "ten_aa", CorrelationID: "corr_ABC123", DecisionID: "dec_01", PrincipalID: "usr_88213", IntentHash: sum("1"),
		CapabilityID: "fin.cap.create_escalation_case", TargetRef: "fin.transaction:TX88213", ParamsHash: sum("2"), EvidenceSetHash: sum("3"), PlanHash: sum("4"), PlanStep: 1,
		TargetVersions: []contracts.DecisionGrantV1TargetVersionsItem{{ResourceRef: "fin.transaction:TX88213", Source: "fin.src.ledger", VersionToken: "v7"}}}, nil
}

func issue(keys *grants.Keys, at time.Duration, seed int64) (contracts.DecisionGrantV1, error) {
	d, err := allowAt(at)
	if err != nil {
		return contracts.DecisionGrantV1{}, err
	}
	s, err := step()
	if err != nil {
		return contracts.DecisionGrantV1{}, err
	}
	i, err := grants.NewIssuer(keys, ids.New(clock.NewManual(epoch), rand.New(rand.NewSource(seed))), rand.New(rand.NewSource(seed+1)), validity)
	if err != nil {
		return contracts.DecisionGrantV1{}, err
	}
	return i.Issue(d, s)
}

// forgeries is the corpus an attacker without the current secrets can present at an offset. Every one must be refused.
func forgeries(p plan, at time.Duration, genuine contracts.DecisionGrantV1, seed int64) ([]contracts.DecisionGrantV1, error) {
	var out []contracts.DecisionGrantV1
	for _, k := range []grants.Key{{ID: oldID, Secret: p.attacker}, {ID: newID, Secret: p.attacker}} { // the right key ids, a secret the attacker made up
		ring, err := grants.NewKeys([]grants.Key{k})
		if err != nil {
			return nil, err
		}
		g, err := issue(ring, at, seed)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	if at >= p.retireAt { // the retired key is gone: a grant signed under it must be refused
		ring, err := grants.NewKeys([]grants.Key{{ID: oldID, Secret: p.oldSecret}})
		if err != nil {
			return nil, err
		}
		g, err := issue(ring, at, seed)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	tampered := genuine
	tampered.ParamsHash = sum("9")
	swapped := genuine
	swapped.Sig.KeyId = oldID
	if genuine.Sig.KeyId == oldID {
		swapped.Sig.KeyId = newID
	}
	unknown := genuine
	unknown.Sig.KeyId = "grant.hmac.9"
	empty := genuine
	empty.Sig.Value = ""
	return append(out, tampered, swapped, unknown, empty), nil
}

// run issues a grant per tick under the ring in force, then verifies every live grant at every tick and presents the forgeries.
func run(p plan) (result, error) {
	var r result
	var all []issued
	for at := time.Duration(0); at < length; at += tick {
		keys, err := p.ring(at)
		if err != nil {
			return r, fmt.Errorf("the ring at %v: %w", at, err)
		}
		g, err := issue(keys, at, int64(at/time.Second)+1)
		if err != nil {
			return r, fmt.Errorf("issue at %v: %w", at, err)
		}
		all = append(all, issued{g: g, at: at, expires: at + validity})
		r.issued++
		switch g.Sig.KeyId {
		case oldID:
			r.signedOld++
		case newID:
			r.signedNew++
		}
	}
	for at := time.Duration(0); at < length+validity; at += tick {
		keys, err := p.ring(at)
		if err != nil {
			return r, fmt.Errorf("the ring at %v: %w", at, err)
		}
		for _, dg := range all {
			if at < dg.at || at >= dg.expires {
				continue
			}
			r.checks++
			if keys.Verify(dg.g) != nil {
				r.rejectedValid++
			}
		}
		if at >= length {
			continue
		}
		fs, err := forgeries(p, at, all[int(at/tick)].g, int64(at/time.Second)+7)
		if err != nil {
			return r, err
		}
		for _, f := range fs {
			r.tried++
			if keys.Verify(f) == nil {
				r.forgedAccepted++
			}
		}
	}
	return r, nil
}

// failures says, in counts, why a run of the safe rule is not a pass. Empty means it passed.
func (r result) failures() []string {
	var out []string
	if r.rejectedValid != 0 {
		out = append(out, fmt.Sprintf("%d valid grants were rejected", r.rejectedValid))
	}
	if r.forgedAccepted != 0 {
		out = append(out, fmt.Sprintf("%d forged grants were accepted", r.forgedAccepted))
	}
	if r.checks < minChecks || r.tried < minForgeries {
		out = append(out, fmt.Sprintf("only %d checks and %d forgeries: not a drill", r.checks, r.tried))
	}
	if r.signedOld == 0 || r.signedNew == 0 || r.signedOld+r.signedNew != r.issued {
		out = append(out, "the run did not sign under both keys")
	}
	return out
}
