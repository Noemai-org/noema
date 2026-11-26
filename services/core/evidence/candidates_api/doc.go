// Package candidates_api: POST /internal/v1/evidence/candidates: principal-scoped, candidate-shaped
// records (candidates-response.v2: no name of any kind, A676) for the model service, at most 20, newest first.
//
// Implements: REQ-CTX-006, REQ-CTX-007, REQ-AMB-002, REQ-SEC-003.
//
// May import: platform/{config,telemetry,redaction}, internal/contracts, evidence/layer.
//
// Must never: carry any field outside the candidate shape (the candidates-api-shape lint); return a
// row the principal does not own; return a full account number or a name of any kind, masked or not.
//
// Built by EVD-04 (ruling A645). `Reader` is the read; the endpoint that serves it to the model service is the composition's. See README.md.
package candidates_api
