// The body a person sends to answer a confirmation request, POST /v1/confirmations/{confirmation_request_id} (PERD 39.2, REQ-TRU-009). A customer answers a SELF confirmation through the
// customer entry point (A433); the identity and the role are the session's, never in the body (INV-12). The path id and the body's request_id must be the same request: the UI refuses a
// mismatch before anything is sent, and the route refuses it as well. No network access here; the page that sends the answer owns that.
import { ContractError, parseConfirmationAnswerV1, type ConfirmationAnswerV1 } from "./contract_types/customer.js";

export function parseConfirmationAnswerForPath(pathRequestId: unknown, body: unknown): ConfirmationAnswerV1 {
  const answer = parseConfirmationAnswerV1(body);
  if (typeof pathRequestId !== "string" || answer.request_id !== pathRequestId) {
    throw new ContractError("$.request_id", "must be the request id of the path");
  }
  return answer;
}
