FROM debian:13-slim

ENV DEBIAN_FRONTEND=noninteractive

ARG GO_VERSION=1.27.1
ENV GOCACHE=/go-cache
ENV PATH=/usr/local/go/bin:$PATH
ENV HOME=/tmp

RUN echo "if you are not in China, please delete this line in Dockerfile" \
    && /usr/bin/sed -i.bak 's|deb.debian.org|mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list.d/debian.sources

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

# use https://go.dev/dl/ instead if you are not in China
RUN curl -fsSL https://golang.google.cn/dl/go${GO_VERSION}.linux-amd64.tar.gz | tar -C /usr/local -xzf -

RUN go telemetry off || true
RUN rm -f "$(go env GOROOT)/libexec/telemetry" || true

RUN mkdir -p /go-cache && chown -R 65534:65534 /go-cache

USER nobody

RUN echo 'package main; import "fmt"; func main(){fmt.Println("ok")}' > /tmp/dummy.go \
    && go build -o /tmp/dummy /tmp/dummy.go \
    && rm -f /tmp/dummy /tmp/dummy.go