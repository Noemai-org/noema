# Build context: services/core/ only (DEC-2026-10-13 R2). Build with: docker build services/core
FROM golang:1.24.7-alpine3.22@sha256:fc2cff6625f3c1c92e6c85938ac5bd09034ad0d4bc2dfb08278020b68540dbb5 AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOFLAGS=-trimpath go build -o /out/core ./cmd/core

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
LABEL noema.service="core"
COPY --from=build /out/core /usr/local/bin/core
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/core"]
