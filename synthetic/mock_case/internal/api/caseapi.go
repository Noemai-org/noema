package api

import (
	"context"
	"net/http"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/auth"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/faultplan"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/store"
)

type route struct {
	method  string
	path    string
	handler http.HandlerFunc
}

func (s *Server) caseRoutes() []route {
	return []route{
		{"GET", "/healthz", s.healthz},
		{"GET", "/v1/openapi.json", s.describe(caseDescription)},
		{"POST", "/v1/cases", s.serve(faultplan.OpCreateCase, s.parseCreateCase)},
		{"GET", "/v1/cases", s.serve(faultplan.OpFindCases, s.parseFindCases)},
		{"GET", "/v1/cases/{case_id}", s.serve(faultplan.OpGetCase, s.parseGetCase)},
		{"POST", "/v1/cases/{case_id}/route", s.serve(faultplan.OpRouteCase, s.parseRouteCase)},
		{"POST", "/v1/cases/{case_id}/complaints", s.serve(faultplan.OpCreateComplaint, s.parseCreateComplaint)},
		{"POST", "/v1/cases/{case_id}/evidence", s.serve(faultplan.OpAppendEvidence, s.parseAppendEvidence)},
		{"GET", "/v1/complaints", s.serve(faultplan.OpFindComplaints, s.parseFindComplaints)},
		{"GET", "/v1/complaints/{complaint_ref}", s.serve(faultplan.OpGetComplaint, s.parseGetComplaint)},
		{"GET", "/v1/evidence", s.serve(faultplan.OpFindEvidence, s.parseFindEvidence)},
		{"GET", "/v1/evidence/{attachment_id}", s.serve(faultplan.OpGetEvidence, s.parseGetEvidence)},
	}
}

// CaseRoutes lists "METHOD path" for every route of the case listener, for the tests that compare it with the
// API description.
func (s *Server) CaseRoutes() []string { return routeNames(s.caseRoutes()) }

func routeNames(rs []route) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.method + " " + r.path
	}
	return out
}

func notFoundRoute(w http.ResponseWriter, r *http.Request) {
	writeErr(w, correlationID(r), newErr(404, "NOT_FOUND", "no such route", false))
}

// noHead gives HEAD no route: the mux would otherwise answer it as the GET it matches, and a request that
// reads state must be one of the described operations.
func noHead(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			notFoundRoute(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CaseHandler is the case API: runtime credential only, /healthz open.
func (s *Server) CaseHandler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range s.caseRoutes() {
		mux.HandleFunc(rt.method+" "+rt.path, rt.handler)
	}
	mux.HandleFunc("/", notFoundRoute) // any other path or method has no route: JSON 404, never 405
	return s.gate(auth.Runtime, map[string]bool{"/healthz": true}, noHead(mux), "case")
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withDeadline(r.Context())
	defer cancel()
	if err := s.st.Ping(ctx); err != nil {
		writeErr(w, "", newErr(503, "UNAVAILABLE", "the database does not answer", true))
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) describe(doc []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCommon(w)
		w.Write(doc) //nolint:errcheck
	}
}

// ---- cases ----

func (s *Server) parseCreateCase(w http.ResponseWriter, r *http.Request) (*opCall, *apiError) {
	key, e := idempotencyKey(r)
	if e != nil {
		return nil, e
	}
	// A create is a conditional write (create_escalation_case is CONDITIONAL_WRITE): it must say what it is conditional on,
	// "no case exists for this subject and epoch", and the target enforces it atomically. An unconditional create has no route.
	switch inm := r.Header.Get("If-None-Match"); inm {
	case "":
		return nil, newErr(428, "PRECONDITION_REQUIRED", "a case is created only if absent: send If-None-Match: *", false)
	case "*":
	default:
		return nil, badRequest("If-None-Match", "only * is supported: create only if absent")
	}
	var b struct {
		TenantID        string  `json:"tenant_id"`
		PrincipalID     string  `json:"principal_id"`
		TransactionRef  string  `json:"transaction_ref"`
		ReasonCode      string  `json:"reason_code"`
		OwnerQueue      string  `json:"owner_queue"`
		Priority        string  `json:"priority"`
		CreatedByIntent *string `json:"created_by_intent"`
		Epoch           *int64  `json:"epoch"`
	}
	if e := decodeBody(w, r, &b); e != nil {
		return nil, e
	}
	for _, c := range []struct {
		field, v string
		long     bool
	}{{"tenant_id", b.TenantID, false}, {"principal_id", b.PrincipalID, false}, {"transaction_ref", b.TransactionRef, false}, {"reason_code", b.ReasonCode, true}} {
		re := idRE
		if c.long {
			re = longRE
		}
		if e := check(re, c.field, c.v); e != nil {
			return nil, e
		}
	}
	if b.CreatedByIntent == nil {
		return nil, badRequest("created_by_intent", "required: the intent that creates the case")
	}
	if e := check(idRE, "created_by_intent", *b.CreatedByIntent); e != nil {
		return nil, e
	}
	if b.Epoch == nil {
		return nil, badRequest("epoch", "required: the logical resolution epoch, an integer from 0")
	}
	if *b.Epoch < 0 || *b.Epoch > maxEpoch {
		return nil, badRequest("epoch", "an integer from 0 to 9007199254740991")
	}
	if b.OwnerQueue == "" {
		b.OwnerQueue = s.cfg.DefaultQueue
	} else if !contains(s.cfg.Queues, b.OwnerQueue) {
		return nil, badRequest("owner_queue", "not a queue of this case system")
	}
	if b.Priority == "" {
		b.Priority = s.cfg.DefaultPriority
	} else if !contains(s.cfg.Priorities, b.Priority) {
		return nil, badRequest("priority", "not a priority of this case system")
	}
	in := store.CreateCaseInput{TenantID: b.TenantID, PrincipalID: b.PrincipalID, TransactionRef: b.TransactionRef,
		ReasonCode: b.ReasonCode, OwnerQueue: b.OwnerQueue, Priority: b.Priority, IdempotencyKey: key,
		CreatedByIntent: *b.CreatedByIntent, Epoch: *b.Epoch}
	return &opCall{
		attrs:  map[string]string{"tenant_id": in.TenantID, "principal_id": in.PrincipalID, "transaction_ref": in.TransactionRef, "idempotency_key": key},
		tenant: in.TenantID, key: key,
		run: func(ctx context.Context, cmd store.Cmd, _ *faultplan.Effect) reply {
			c, oc, err := s.st.CreateCase(ctx, cmd, in)
			if err != nil {
				return s.dbFailure(cmd.Operation, err)
			}
			return outcomeReply(oc, c, "case")
		},
	}, nil
}

func (s *Server) parseFindCases(_ http.ResponseWriter, r *http.Request) (*opCall, *apiError) {
	q, e := queryParams(r, "tenant_id", "idempotency_key", "principal_id", "transaction_ref", "limit", "cursor")
	if e != nil {
		return nil, e
	}
	if e := check(idRE, "tenant_id", q["tenant_id"]); e != nil {
		return nil, e
	}
	f := store.CaseFilter{TenantID: q["tenant_id"], IdempotencyKey: q["idempotency_key"], PrincipalID: q["principal_id"], TransactionRef: q["transaction_ref"]}
	if f.IdempotencyKey == "" && f.PrincipalID == "" {
		return nil, badRequest("principal_id", "give idempotency_key or principal_id: cases are never searched globally")
	}
	if f.TransactionRef != "" && f.PrincipalID == "" {
		return nil, badRequest("principal_id", "a transaction search is scoped to a principal")
	}
	for _, c := range []struct {
		field string
		re    interface{ MatchString(string) bool }
		v     string
	}{{"idempotency_key", keyRE, f.IdempotencyKey}, {"principal_id", idRE, f.PrincipalID}, {"transaction_ref", idRE, f.TransactionRef}} {
		if c.v != "" && !c.re.MatchString(c.v) {
			return nil, badRequest(c.field, "malformed")
		}
	}
	after, e := decodeCursor(q["cursor"])
	if e != nil {
		return nil, e
	}
	limit := q["limit"]
	if _, e := pageSize(limit, nil); e != nil {
		return nil, e
	}
	return &opCall{
		attrs:  map[string]string{"tenant_id": f.TenantID, "principal_id": f.PrincipalID, "transaction_ref": f.TransactionRef, "idempotency_key": f.IdempotencyKey},
		tenant: f.TenantID, key: f.IdempotencyKey,
		run: func(ctx context.Context, cmd store.Cmd, eff *faultplan.Effect) reply {
			n, _ := pageSize(limit, eff)
			items, more, err := s.st.ListCases(ctx, cmd, f, after, n)
			if err != nil {
				return s.dbFailure(cmd.Operation, err)
			}
			if items == nil {
				items = []store.Case{}
			}
			last := int64(0)
			if len(items) > 0 {
				last = items[len(items)-1].Seq
			}
			return reply{status: 200, body: newPage(items, more, last)}
		},
	}, nil
}

func (s *Server) parseGetCase(_ http.ResponseWriter, r *http.Request) (*opCall, *apiError) {
	q, e := queryParams(r, "tenant_id")
	if e != nil {
		return nil, e
	}
	tenant, id := q["tenant_id"], r.PathValue("case_id")
	if e := check(idRE, "tenant_id", tenant); e != nil {
		return nil, e
	}
	if e := check(idRE, "case_id", id); e != nil {
		return nil, e
	}
	return &opCall{
		attrs: map[string]string{"tenant_id": tenant, "case_id": id}, tenant: tenant, caseID: id,
		run: func(ctx context.Context, cmd store.Cmd, _ *faultplan.Effect) reply {
			c, oc, err := s.st.GetCase(ctx, cmd, tenant, id)
			if err != nil {
				return s.dbFailure(cmd.Operation, err)
			}
			return outcomeReply(oc, c, "case")
		},
	}, nil
}

func (s *Server) parseRouteCase(w http.ResponseWriter, r *http.Request) (*opCall, *apiError) {
	var b struct {
		TenantID             string `json:"tenant_id"`
		OwnerQueue           string `json:"owner_queue"`
		ExpectedVersionToken string `json:"expected_version_token"`
	}
	if e := decodeBody(w, r, &b); e != nil {
		return nil, e
	}
	id := r.PathValue("case_id")
	if e := check(idRE, "case_id", id); e != nil {
		return nil, e
	}
	if e := check(idRE, "tenant_id", b.TenantID); e != nil {
		return nil, e
	}
	if !contains(s.cfg.Queues, b.OwnerQueue) {
		return nil, badRequest("owner_queue", "not a queue of this case system")
	}
	expected, err := store.ParseVersionToken(b.ExpectedVersionToken)
	if err != nil {
		return nil, badRequest("expected_version_token", "missing or not a version token")
	}
	return &opCall{
		attrs: map[string]string{"tenant_id": b.TenantID, "case_id": id}, tenant: b.TenantID, caseID: id,
		run: func(ctx context.Context, cmd store.Cmd, eff *faultplan.Effect) reply {
			touch := eff != nil && eff.Name == faultplan.EffectConcurrentModification
			c, oc, err := s.st.RouteCase(ctx, cmd, b.TenantID, id, b.OwnerQueue, expected, touch)
			if err != nil {
				return s.dbFailure(cmd.Operation, err)
			}
			if oc == store.VersionMismatch {
				// A distinct error; it applied nothing the caller asked for. It names the token to read from.
				return reply{err: newErr(409, "VERSION_MISMATCH", "the case has changed since the version token was read", false,
					errDetail{"expected_version_token", "stale"}, errDetail{"current_version_token", c.VersionToken})}
			}
			return outcomeReply(oc, c, "case")
		},
	}, nil
}

// ---- complaints ----

func (s *Server) parseCreateComplaint(w http.ResponseWriter, r *http.Request) (*opCall, *apiError) {
	key, e := idempotencyKey(r)
	if e != nil {
		return nil, e
	}
	var b struct {
		TenantID string `json:"tenant_id"`
	}
	if e := decodeBody(w, r, &b); e != nil {
		return nil, e
	}
	id := r.PathValue("case_id")
	if e := check(idRE, "case_id", id); e != nil {
		return nil, e
	}
	if e := check(idRE, "tenant_id", b.TenantID); e != nil {
		return nil, e
	}
	return &opCall{
		attrs: map[string]string{"tenant_id": b.TenantID, "case_id": id, "idempotency_key": key}, tenant: b.TenantID, caseID: id, key: key,
		run: func(ctx context.Context, cmd store.Cmd, _ *faultplan.Effect) reply {
			c, oc, err := s.st.CreateComplaint(ctx, cmd, b.TenantID, id, key)
			if err != nil {
				return s.dbFailure(cmd.Operation, err)
			}
			return outcomeReply(oc, c, "case")
		},
	}, nil
}

func (s *Server) parseFindComplaints(_ http.ResponseWriter, r *http.Request) (*opCall, *apiError) {
	q, e := queryParams(r, "tenant_id", "idempotency_key", "case_id", "limit", "cursor")
	if e != nil {
		return nil, e
	}
	tenant, key, caseID := q["tenant_id"], q["idempotency_key"], q["case_id"]
	if e := check(idRE, "tenant_id", tenant); e != nil {
		return nil, e
	}
	if key == "" && caseID == "" {
		return nil, badRequest("case_id", "give idempotency_key or case_id")
	}
	if (key != "" && !keyRE.MatchString(key)) || (caseID != "" && !idRE.MatchString(caseID)) {
		return nil, badRequest("idempotency_key", "malformed")
	}
	after, e := decodeCursor(q["cursor"])
	if e != nil {
		return nil, e
	}
	limit := q["limit"]
	if _, e := pageSize(limit, nil); e != nil {
		return nil, e
	}
	return &opCall{
		attrs: map[string]string{"tenant_id": tenant, "case_id": caseID, "idempotency_key": key}, tenant: tenant, caseID: caseID, key: key,
		run: func(ctx context.Context, cmd store.Cmd, eff *faultplan.Effect) reply {
			n, _ := pageSize(limit, eff)
			items, more, err := s.st.ListComplaints(ctx, cmd, tenant, key, caseID, after, n)
			if err != nil {
				return s.dbFailure(cmd.Operation, err)
			}
			if items == nil {
				items = []store.Complaint{}
			}
			last := int64(0)
			if len(items) > 0 {
				last = items[len(items)-1].Seq
			}
			return reply{status: 200, body: newPage(items, more, last)}
		},
	}, nil
}

func (s *Server) parseGetComplaint(_ http.ResponseWriter, r *http.Request) (*opCall, *apiError) {
	q, e := queryParams(r, "tenant_id")
	if e != nil {
		return nil, e
	}
	tenant, ref := q["tenant_id"], r.PathValue("complaint_ref")
	if e := check(idRE, "tenant_id", tenant); e != nil {
		return nil, e
	}
	if e := check(idRE, "complaint_ref", ref); e != nil {
		return nil, e
	}
	return &opCall{
		attrs: map[string]string{"tenant_id": tenant}, tenant: tenant,
		run: func(ctx context.Context, cmd store.Cmd, _ *faultplan.Effect) reply {
			c, oc, err := s.st.GetComplaint(ctx, cmd, tenant, ref)
			if err != nil {
				return s.dbFailure(cmd.Operation, err)
			}
			return outcomeReply(oc, c, "complaint")
		},
	}, nil
}

// ---- evidence ----

func (s *Server) parseAppendEvidence(w http.ResponseWriter, r *http.Request) (*opCall, *apiError) {
	key, e := idempotencyKey(r)
	if e != nil {
		return nil, e
	}
	var b struct {
		TenantID    string `json:"tenant_id"`
		Payload     string `json:"payload"`
		PayloadHash string `json:"payload_hash"`
	}
	if e := decodeBody(w, r, &b); e != nil {
		return nil, e
	}
	id := r.PathValue("case_id")
	if e := check(idRE, "case_id", id); e != nil {
		return nil, e
	}
	if e := check(idRE, "tenant_id", b.TenantID); e != nil {
		return nil, e
	}
	if b.Payload == "" {
		return nil, badRequest("payload", "missing: the attachment content, as the exact string to be hashed")
	}
	if b.PayloadHash != "" {
		if !hashRE.MatchString(b.PayloadHash) {
			return nil, badRequest("payload_hash", "must be sha256: followed by 64 lower-case hex digits")
		}
		if b.PayloadHash != store.PayloadHash(b.Payload) {
			return nil, newErr(422, "PAYLOAD_HASH_MISMATCH", "payload_hash is not the SHA-256 of payload", false, errDetail{"payload_hash", "does not match payload"})
		}
	}
	return &opCall{
		attrs: map[string]string{"tenant_id": b.TenantID, "case_id": id, "idempotency_key": key}, tenant: b.TenantID, caseID: id, key: key,
		run: func(ctx context.Context, cmd store.Cmd, _ *faultplan.Effect) reply {
			a, oc, err := s.st.AppendEvidence(ctx, cmd, b.TenantID, id, key, b.Payload)
			if err != nil {
				return s.dbFailure(cmd.Operation, err)
			}
			return outcomeReply(oc, a, "case")
		},
	}, nil
}

func (s *Server) parseFindEvidence(_ http.ResponseWriter, r *http.Request) (*opCall, *apiError) {
	q, e := queryParams(r, "tenant_id", "idempotency_key", "case_id", "limit", "cursor")
	if e != nil {
		return nil, e
	}
	tenant, key, caseID := q["tenant_id"], q["idempotency_key"], q["case_id"]
	if e := check(idRE, "tenant_id", tenant); e != nil {
		return nil, e
	}
	if key == "" && caseID == "" {
		return nil, badRequest("case_id", "give idempotency_key or case_id")
	}
	if (key != "" && !keyRE.MatchString(key)) || (caseID != "" && !idRE.MatchString(caseID)) {
		return nil, badRequest("idempotency_key", "malformed")
	}
	after, e := decodeCursor(q["cursor"])
	if e != nil {
		return nil, e
	}
	limit := q["limit"]
	if _, e := pageSize(limit, nil); e != nil {
		return nil, e
	}
	return &opCall{
		attrs: map[string]string{"tenant_id": tenant, "case_id": caseID, "idempotency_key": key}, tenant: tenant, caseID: caseID, key: key,
		run: func(ctx context.Context, cmd store.Cmd, eff *faultplan.Effect) reply {
			n, _ := pageSize(limit, eff)
			items, more, err := s.st.ListEvidence(ctx, cmd, tenant, key, caseID, after, n)
			if err != nil {
				return s.dbFailure(cmd.Operation, err)
			}
			if items == nil {
				items = []store.Attachment{}
			}
			last := int64(0)
			if len(items) > 0 {
				last = items[len(items)-1].Seq
			}
			return reply{status: 200, body: newPage(items, more, last)}
		},
	}, nil
}

func (s *Server) parseGetEvidence(_ http.ResponseWriter, r *http.Request) (*opCall, *apiError) {
	q, e := queryParams(r, "tenant_id")
	if e != nil {
		return nil, e
	}
	tenant, id := q["tenant_id"], r.PathValue("attachment_id")
	if e := check(idRE, "tenant_id", tenant); e != nil {
		return nil, e
	}
	if e := check(idRE, "attachment_id", id); e != nil {
		return nil, e
	}
	return &opCall{
		attrs: map[string]string{"tenant_id": tenant}, tenant: tenant,
		run: func(ctx context.Context, cmd store.Cmd, _ *faultplan.Effect) reply {
			a, oc, err := s.st.GetEvidence(ctx, cmd, tenant, id)
			if err != nil {
				return s.dbFailure(cmd.Operation, err)
			}
			return outcomeReply(oc, a, "attachment")
		},
	}, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
