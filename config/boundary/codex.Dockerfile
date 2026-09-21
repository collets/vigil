# The build context is assembled by scripts/boundary/build-codex.py from an
# exact installed architecture package. Credentials and native state are never
# copied into the image.
FROM alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0
RUN apk add --no-cache ca-certificates git
COPY codex /opt/codex/codex
COPY rg /usr/local/bin/rg
COPY vigil-guardian vigil-worker /opt/vigil/
RUN chmod 0755 /opt/codex/codex /usr/local/bin/rg /opt/vigil/vigil-guardian /opt/vigil/vigil-worker && \
    mkdir -p /native/codex /work /control /relay
ENV HOME=/native CODEX_HOME=/native/codex PATH=/opt/codex:/usr/local/bin:/usr/bin:/bin
WORKDIR /work
ENTRYPOINT ["/opt/vigil/vigil-guardian"]
