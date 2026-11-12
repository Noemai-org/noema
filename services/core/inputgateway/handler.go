package inputgateway

import (
	"errors"
	"io"
	"net/http"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// Route is the path of the gateway in net/http pattern form. The composition root mounts it behind the session check and the customer role gate.
const Route = "POST /v1/conversations"

// Handler serves Route. resolve returns the identity of the authenticated session of the request (the edge's session check has already run) or false when there is
// none; the handler takes nothing about identity from the query, the headers or the body.
func (g *Gateway) Handler(resolve func(*http.Request) (Identity, bool)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			g.fail(w, http.StatusMethodNotAllowed, contracts.ErrorsV2ErrorCodeINPUTINVALID, "Use POST.", false)
			return
		}
		id, ok := resolve(r)
		if !ok {
			g.fail(w, http.StatusUnauthorized, contracts.ErrorsV2ErrorCodeUNAUTHENTICATED, "A session is required.", false)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, g.maxBody))
		if err != nil {
			g.fail(w, http.StatusRequestEntityTooLarge, contracts.ErrorsV2ErrorCodeINPUTINVALID, "The request body is too large or cannot be read.", false)
			return
		}
		res, err := g.Accept(r.Context(), id, raw)
		switch {
		case errors.Is(err, ErrRequestIDReused):
			g.fail(w, http.StatusConflict, contracts.ErrorsV2ErrorCodeREQUESTIDREUSED, "This request id was used before with another body.", false)
		case errors.Is(err, ErrInvalidInput):
			g.fail(w, http.StatusBadRequest, contracts.ErrorsV2ErrorCodeINPUTINVALID, "The request body is not valid.", false)
		case errors.Is(err, ErrNoIdentity):
			g.fail(w, http.StatusUnauthorized, contracts.ErrorsV2ErrorCodeSESSIONINVALID, "The session is not valid.", false)
		case errors.Is(err, ErrRecord), errors.Is(err, ErrStart):
			g.fail(w, http.StatusServiceUnavailable, contracts.ErrorsV2ErrorCodeDEPENDENCYUNAVAILABLE, "The request cannot be taken now.", true)
		case err != nil:
			g.fail(w, http.StatusInternalServerError, contracts.ErrorsV2ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be taken.", false)
		default:
			status := http.StatusAccepted
			if res.Deduplicated {
				status = http.StatusOK
			}
			g.write(w, status, map[string]any{"correlation_id": res.CorrelationID, "input_event_id": res.Input.EventID, "input_seq": res.Input.Seq, "deduplicated": res.Deduplicated})
		}
	})
}

func (g *Gateway) write(w http.ResponseWriter, status int, body map[string]any) {
	b, err := crypto.CanonicalJSON(body)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// fail writes the error envelope of errors.v2. It carries a code and a fixed sentence, never a part of the input.
func (g *Gateway) fail(w http.ResponseWriter, status int, code contracts.ErrorsV2ErrorCode, message string, retryable bool) {
	corr, err := g.ids.ID("corr")
	if err != nil {
		corr = "corr_unavailable"
	}
	g.write(w, status, map[string]any{"error": map[string]any{"code": string(code), "message": message, "retryable": retryable, "correlation_id": corr}})
}
