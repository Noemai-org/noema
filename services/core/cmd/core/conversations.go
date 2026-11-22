package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// conversations.go: the answer of POST /v1/conversations (ruling A234, interim, docs#187, relayed, not yet committed; noema#601). The Input Gateway answers with the recorded input's
// reference; PERD 39.2 promises the investigation. This file turns the one into the other without changing the gateway: on success it answers the PERD shape plus the correlation ID,
// derived from the correlation ID alone so a retry (which gets the original correlation ID, A168) gets the same values. Every other response is the gateway's, byte for byte, unless it is longer than maxResponseBytes (A457): then it is the fixed 502 of failUpstream (A471, interim, relayed, recorded in docs#216, not yet merged).

// publicStageUnderstanding is the state of a new investigation: Noema is reading the request (the public stage of rulings A187, A207, A211).
const publicStageUnderstanding = "understanding"

// ulidCorrelation is a correlation ID the core minted: `corr_` and a ULID (26 Crockford base-32 characters: no I, L, O or U).
var ulidCorrelation = regexp.MustCompile(`\Acorr_([0-9A-HJKMNP-TV-Z]{26})\z`)

// investigationIDOf is A234: `inv_` and the ULID of the correlation ID. `conversation_ref` is the same value: PERD 32.3 has no other prefix for it, and `canonical-input.v2`
// types it as `common.v1`'s InvestigationId.
func investigationIDOf(correlationID string) (string, error) {
	m := ulidCorrelation.FindStringSubmatch(correlationID)
	if m == nil {
		return "", errors.New("cmd/core: the correlation ID is not corr_ and a ULID")
	}
	return "inv_" + m[1], nil
}

// maxResponseBytes is the UI transport limit (A446, A457, interim, relayed, recorded in docs#213, not yet merged): no response of this route is longer.
const maxResponseBytes = 65536

// boundedCorrelation is the correlation ID an error may echo.
func boundedCorrelation(corr string) string {
	if !echoableCorrelation.MatchString(corr) {
		return "corr_NONE"
	}
	return corr
}

// echoableCorrelation is the shape the edge mints (edge/session): short, and no character that needs escaping.
var echoableCorrelation = regexp.MustCompile(`\Acorr_[0-9A-Za-z]{2,32}\z`)

// capture is a ResponseWriter that holds what a handler wrote, so the route can decide what to send.
// It keeps at most maxResponseBytes+1 bytes: one byte past the limit is enough to know the answer is too long, and nothing more is buffered.
type capture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

// tooLong is true once the handler has written more than the limit.
func (c *capture) tooLong() bool { return c.body.Len() > maxResponseBytes }

func (c *capture) Header() http.Header  { return c.header }
func (c *capture) WriteHeader(code int) { c.status = code }
func (c *capture) Write(b []byte) (int, error) {
	room := maxResponseBytes + 1 - c.body.Len()
	if room <= 0 {
		return 0, errResponseTooLong
	}
	if len(b) > room {
		c.body.Write(b[:room])
		return room, errResponseTooLong
	}
	return c.body.Write(b)
}

// errResponseTooLong tells the handler to stop writing.
var errResponseTooLong = errors.New("cmd/core: the response is longer than the transport limit")

// startConversation wraps the gateway's handler. The capture starts from a copy of the response's headers: the gateway reads the request's correlation ID from them, and the error body, the header and the logs must agree. On 200 and 202 it replaces the body with the A234 answer; any other status, header and body pass through unchanged. If the
// gateway's answer cannot be read, or its correlation ID is not one the core minted, the answer is an error and never a guess.
func startConversation(inner http.Handler) http.Handler {
	return startConversationWith(inner, conversationAnswer)
}

// answerFunc derives the answer of a conversation start from the correlation ID the gateway minted.
type answerFunc func(correlationID string) (map[string]any, error)

// conversationAnswer is the answer of A234: the five members, all derived from the one correlation ID.
func conversationAnswer(correlationID string) (map[string]any, error) {
	id, err := investigationIDOf(correlationID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"investigation_id": id, "conversation_ref": id, "state": publicStageUnderstanding,
		"stream_url": "/v1/conversations/" + id + "/events", "correlation_id": correlationID,
	}, nil
}

// errAnswerInconsistent is the refusal of checkAnswer. Its text names no member and no value (AGENT-13, A347).
var errAnswerInconsistent = errors.New("cmd/core: the answer's members do not name one investigation")

// checkAnswer is the writer's own check of the relations the schema cannot state (A479; the validator's check is tools/contracts/validate.py, on the same fixtures): conversation_ref is the
// value of investigation_id, stream_url is the events route of it, and correlation_id is corr_ and its ULID. An answer that is not made of these five strings is refused.
func checkAnswer(a map[string]any) error {
	text := func(member string) (string, bool) { v, ok := a[member].(string); return v, ok }
	id, ok := text("investigation_id")
	if !ok || !strings.HasPrefix(id, "inv_") {
		return errAnswerInconsistent
	}
	for member, want := range map[string]string{
		"conversation_ref": id,
		"stream_url":       "/v1/conversations/" + id + "/events",
		"correlation_id":   "corr_" + strings.TrimPrefix(id, "inv_"),
	} {
		if got, ok := text(member); !ok || got != want {
			return errAnswerInconsistent
		}
	}
	return nil
}

func startConversationWith(inner http.Handler, derive answerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := &capture{header: w.Header().Clone(), status: http.StatusOK}
		inner.ServeHTTP(c, r)
		if c.tooLong() {
			failUpstream(w, r)
			return
		}
		if c.status != http.StatusOK && c.status != http.StatusAccepted {
			for k, v := range c.header {
				w.Header()[k] = v
			}
			w.WriteHeader(c.status)
			_, _ = w.Write(c.body.Bytes())
			return
		}
		var got struct {
			CorrelationID string `json:"correlation_id"`
		}
		var answer map[string]any
		err := json.Unmarshal(c.body.Bytes(), &got)
		if err == nil {
			answer, err = derive(got.CorrelationID)
		}
		if err == nil {
			err = checkAnswer(answer)
		}
		if err != nil {
			failStart(w, r)
			return
		}
		body, err := crypto.CanonicalJSON(answer)
		if err != nil {
			failStart(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(c.status)
		_, _ = w.Write(body)
	})
}

// failStart is the answer when the gateway accepted the input but its answer cannot be turned into an investigation: the envelope of every other internal failure, with the
// request's correlation ID and no detail (AGENT-13).
func failStart(w http.ResponseWriter, r *http.Request) {
	corr := boundedCorrelation(session.CorrelationFromContext(r.Context()))
	body, err := crypto.CanonicalJSON(map[string]any{"error": map[string]any{"code": string(contracts.ErrorsV2ErrorCodeINTERNALSAFETYBLOCK), "message": "The request cannot be taken.", "retryable": false, "correlation_id": corr}})
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write(body)
}

// failUpstream is the answer when the gateway's answer is longer than maxResponseBytes (A471): 500 is for our own faults, so an over-long upstream answer is 502 with the public code the
// gateway already uses for an upstream failure (inputgateway/handler.go:45), fixed text, no upstream text, and the request's bounded correlation ID. The code is retryable by its definition (errors.v3).
func failUpstream(w http.ResponseWriter, r *http.Request) {
	corr := boundedCorrelation(session.CorrelationFromContext(r.Context()))
	body, err := crypto.CanonicalJSON(map[string]any{"error": map[string]any{"code": string(contracts.ErrorsV2ErrorCodeDEPENDENCYUNAVAILABLE), "message": "The request cannot be taken now.", "retryable": true, "correlation_id": corr}})
	if err != nil {
		http.Error(w, "", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_, _ = w.Write(body)
}
