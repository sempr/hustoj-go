FROM golang:1.25.3-alpine
USER nobody
ENV GOCACHE=/tmp/go-build-cache
ADD demo.go /tmp/demo.go
RUN go build -o /tmp/demo /tmp/demo.go && rm /tmp/demo
RUN chown 65534:65534 /tmp/go-build-cache
