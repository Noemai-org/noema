# Build context: synthetic/webhook_relay/ only (DEC-2026-10-13 R2). Build with: docker build synthetic/webhook_relay
# The webhook relay is a test-environment component, not a Noema deployable (DEC-2026-10-16 E5). No dependency beyond
# the Go standard library.
FROM golang:1.24.7-alpine3.22@sha256:fc2cff6625f3c1c92e6c85938ac5bd09034ad0d4bc2dfb08278020b68540dbb5 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOFLAGS=-trimpath go build -o /out/webhook-relay ./cmd/webhook-relay

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
LABEL noema.service="webhook-relay"
COPY --from=build /out/webhook-relay /usr/local/bin/webhook-relay
USER nonroot:nonroot
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/webhook-relay"]
