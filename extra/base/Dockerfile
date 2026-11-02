FROM debian:13-slim

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    gcc g++ rustc \
    python3 python3-pip \
    perl php-cli \
    mono-mcs mono-runtime \
    ruby lua5.4 \
    fp-compiler \
    wget ca-certificates \
 && rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    ARCH="$(dpkg --print-architecture)"; \
    case "$ARCH" in amd64) GOARCH=amd64;; arm64) GOARCH=arm64;; *) echo "unsupported arch: $ARCH" >&2; exit 1;; esac; \
    GO_VERSION="1.27.3"; \
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz" -O /tmp/go.tar.gz; \
    tar -C /usr/local -xzf /tmp/go.tar.gz; \
    rm /tmp/go.tar.gz; \
    ln -sf /usr/local/go/bin/go /usr/bin/go; \
    ln -sf /usr/local/go/bin/gofmt /usr/bin/gofmt; \
    mkdir -p /go-cache; \
    GOCACHE=/go-cache GOFLAGS='-buildvcs=false' go build -o /dev/null -v std 2>/dev/null || true; \
    GOCACHE=/go-cache GOFLAGS='-buildvcs=false' go build -o /dev/null -v runtime 2>/dev/null || true