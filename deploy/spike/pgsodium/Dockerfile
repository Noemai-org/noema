# SEC-12 spike (test infrastructure only): PostgreSQL 17.11 on Alpine, the major and base image Compose runs today,
# plus libsodium and pgsodium built from pinned sources. Nothing here is adopted: adoption needs W4 (EXE-08) and
# a founder decision (AGENT-14). Every download is checked against a pin; nothing is fetched unchecked.
#
# libsodium: the SHA-256 below is of libsodium-1.0.22.tar.gz. Its minisign signature was verified against the
# project's published key RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3 on 2026-11-01 (see README.md).
# pgsodium: a full commit id, checked after fetch (v3.1.11).
FROM postgres:17.11-alpine@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24 AS build
ARG LIBSODIUM_VERSION=1.0.22
ARG LIBSODIUM_SHA256=adbdd8f16149e81ac6078a03aca6fc03b592b89ef7b5ed83841c086191be3349
ARG PGSODIUM_COMMIT=a8b8a2739bab1f7a07445b1c911e2bd1abf67784
RUN apk add --no-cache build-base git curl
WORKDIR /build
RUN curl -fsSL -o libsodium.tar.gz "https://github.com/jedisct1/libsodium/releases/download/${LIBSODIUM_VERSION}-RELEASE/libsodium-${LIBSODIUM_VERSION}.tar.gz" \
 && echo "${LIBSODIUM_SHA256}  libsodium.tar.gz" | sha256sum -c - \
 && tar -xzf libsodium.tar.gz \
 && cd "libsodium-${LIBSODIUM_VERSION}" \
 && ./configure --prefix=/usr/local \
 && make -j"$(nproc)" \
 && make install
RUN git init -q pgsodium \
 && cd pgsodium \
 && git remote add origin https://github.com/michelp/pgsodium \
 && git fetch -q --depth 1 origin "${PGSODIUM_COMMIT}" \
 && git checkout -q --detach FETCH_HEAD \
 && test "$(git rev-parse HEAD)" = "${PGSODIUM_COMMIT}" \
 && make with_llvm=no \
 && make with_llvm=no install

FROM postgres:17.11-alpine@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24
COPY --from=build /usr/local/lib/libsodium.so* /usr/local/lib/
COPY --from=build /usr/local/lib/postgresql/pgsodium.so /usr/local/lib/postgresql/pgsodium.so
COPY --from=build /usr/local/share/postgresql/extension/pgsodium* /usr/local/share/postgresql/extension/
