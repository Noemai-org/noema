# Build context: benchmark/hostility_proxy/ only (DEC-2026-10-13 R2). Build with: docker build benchmark/hostility_proxy
# The hostility proxy is a test-environment component, not a Noema deployable (DEC-2026-10-16 E5). It refuses to
# start outside the bench, ci and test profiles. No dependency beyond the Go standard library.
FROM golang:1.24.7-alpine3.22@sha256:fc2cff6625f3c1c92e6c85938ac5bd09034ad0d4bc2dfb08278020b68540dbb5 AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOFLAGS=-trimpath go build -o /out/hostility-proxy ./cmd/hostility-proxy

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
LABEL noema.service="hostility-proxy"
COPY --from=build /out/hostility-proxy /usr/local/bin/hostility-proxy
USER nonroot:nonroot
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/hostility-proxy"]
