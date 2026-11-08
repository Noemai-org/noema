# Draft message to Tushe: the address of Noema's webhook receiver

Status: DRAFT for the Founder to send, only after `curl https://$RELAY_HOSTNAME/healthz` answers `200`; the Founder fills `<RELAY_HOSTNAME>` below from the GitHub Environment variable of that name (DEC-2026-11-05 D5: the address is not written in any repository file) (deploy/relay-host/README.md, step 8). Written 2026-11-06. SYNTHETIC test environment. It contains no internal strategy, benchmark detail or hidden test material and must stay that way (AGENTS.md section 11, frozen external documents).

---

Subject: Noema webhook receiver address (R13)

Hello,

You asked for our HTTPS URL for the webhooks kept active on our tenants. It is:

**https://<RELAY_HOSTNAME>/webhooks**

**Host.** `<RELAY_HOSTNAME>`, one small public server that runs only the receiver. It is a test endpoint for this environment; if the address ever has to change we will tell you before it does.

**TLS.** HTTPS only. The certificate is issued by a public CA (Let's Encrypt, with ZeroSSL as the fallback the TLS server may use) and renews automatically; TLS 1.2 or 1.3. Plain HTTP on port 80 only answers certificate challenges and redirects to HTTPS.

**Health.** `GET https://<RELAY_HOSTNAME>/healthz` answers `200` with no data. It is only a liveness check, not a test of the receiver (we know you send no synthetic ping, and we have not asked for one).

**What we expect to receive.** `POST /webhooks`, `Content-Type: application/json`, a Standard Webhooks v1 delivery: the headers `webhook-id`, `webhook-timestamp` and `webhook-signature` (`v1,<base64>`), the signature being HMAC-SHA-256 over `<webhook-id>.<webhook-timestamp>.<body>` using the exact body bytes. One or more signatures separated by spaces are accepted, so a secret can be rotated. The timestamp must be within five minutes of our clock, in either direction.

**What we answer.**
- `200` once the delivery is verified and stored durably. A replay of the same `webhook-id` with the same body is also `200` and is stored once.
- `409` if the same `webhook-id` arrives with a different body (not stored).
- `400` for anything that fails verification: a bad or missing signature or header, a timestamp outside five minutes. The answer carries no detail, and the delivery is not stored.
Please retry any non-`2xx` under your normal policy.

**What we need from you.**
1. Who generates the signing secret for this endpoint: do you generate it and give it to us, or do we generate it and give it to you? Please tell us in writing, with the secret's format and how a rotation is announced. Whichever way, the secret should travel by the channel we use for credentials, not by ordinary email in clear text. Until you answer, we use a synthetic secret that only our receiver holds.
2. Confirmation of which of our tenants have this URL registered (R13: webhooks kept active alongside the event feed).
3. The event types that will be delivered to it, so we can check we are not missing one.
4. A way to replay a delivery from your side (your deck says any delivery can be replayed); we will use it to check that a duplicate is stored once.
5. If you publish source address ranges for webhook deliveries, please tell us. We do not allow-list by address today, and nothing depends on your answer.

Thank you.