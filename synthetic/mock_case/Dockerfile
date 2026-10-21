# Build context: synthetic/mock_case/ only (DEC-2026-10-13 R2). Build with: docker build synthetic/mock_case
# mock-case is a test-environment component, not a Noema deployable (DEC-2026-10-16 E5). SYNTHETIC.
FROM golang:1.24.7-alpine3.22@sha256:fc2cff6625f3c1c92e6c85938ac5bd09034ad0d4bc2dfb08278020b68540dbb5 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOFLAGS=-trimpath go build -o /out/mock-case ./cmd/mock-case

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
LABEL noema.service="mock-case"
COPY --from=build /out/mock-case /usr/local/bin/mock-case
USER nonroot:nonroot
EXPOSE 8090 8091
ENTRYPOINT ["/usr/local/bin/mock-case"]
