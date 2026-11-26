package api

import (
	"encoding/json"
	"net/http"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/webhook"
)

// The bench control API can make mock-case deliver a webhook to the relay (A682). It is a bench route: only the bench credential opens it, and mock-case signs with its own SYNTHETIC key
// (never Tushe's and never a listener credential). The body is whatever object the caller gives; mock-case invents no event shape, because Tushe's body members and event names are not stated
// in any governing document. The signing timestamp is the wall clock (the relay checks five minutes against its own clock), not the SYNTHETIC clock.

// hooks builds the sender from the configuration, or returns nil when the webhook settings are absent or unusable. An unusable setting is logged without a value.
func buildHooks(s *Server) *webhook.Sender {
	if s.cfg == nil || s.cfg.WebhookTarget == "" {
		return nil
	}
	sg, err := webhook.NewSigner(s.cfg.WebhookSecret)
	if err != nil {
		s.log.Error("webhook signing secret is not usable: deliveries are off", "err", err.Error())
		return nil
	}
	snd, err := webhook.NewSender(s.cfg.WebhookTarget, sg, webhook.Options{})
	if err != nil {
		s.log.Error("webhook target is not usable: deliveries are off", "err", err.Error())
		return nil
	}
	return snd
}

type deliverBody struct {
	MessageID string          `json:"message_id"`
	Payload   json.RawMessage `json:"payload"`
}

func (s *Server) webhookOff(w http.ResponseWriter) {
	writeErr(w, "", newErr(404, "WEBHOOK_NOT_CONFIGURED", "this mock-case has no webhook target and signing secret", false))
}

func (s *Server) deliverWebhook(w http.ResponseWriter, r *http.Request) {
	if s.hooks == nil {
		s.webhookOff(w)
		return
	}
	var b deliverBody
	if e := decodeBody(w, r, &b); e != nil {
		writeErr(w, "", e)
		return
	}
	if len(b.Payload) == 0 {
		writeErr(w, "", badRequest("payload", "a JSON object is required"))
		return
	}
	res, err := s.hooks.Deliver(r.Context(), b.MessageID, []byte(b.Payload))
	s.answerWebhook(w, b.MessageID, res, err)
}

func (s *Server) replayWebhook(w http.ResponseWriter, r *http.Request) {
	if s.hooks == nil {
		s.webhookOff(w)
		return
	}
	id := r.PathValue("message_id")
	res, err := s.hooks.Replay(r.Context(), id)
	s.answerWebhook(w, id, res, err)
}

// answerWebhook reports what the relay answered as it is: a refusal or a server error from the relay is a 200 here with the relay's status, because mock-case's control call did what it was
// asked; only a delivery that got no answer, or that mock-case itself refused, is an error of this call.
func (s *Server) answerWebhook(w http.ResponseWriter, id string, res webhook.Result, err error) {
	if err != nil {
		s.log.Warn("webhook delivery failed", "message_id", id)
		writeErr(w, "", newErr(422, "WEBHOOK_NOT_DELIVERED", err.Error(), false))
		return
	}
	writeJSON(w, 200, map[string]any{"message_id": id, "relay_status": res.Status, "accepted": res.Accepted, "signed_at": "WALL_CLOCK"})
}
