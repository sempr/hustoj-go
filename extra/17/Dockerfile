FROM golang:1.25.3-alpine

ENV GOCACHE=/go-cache
ENV PATH=/usr/local/go/bin:$PATH
# 创建缓存目录并 chown 给 nobody 用户
RUN mkdir -p /go-cache && chown -R 65534:65534 /go-cache
# 切换到 nobody 用户
USER nobody
# 预热缓存：编译一个简单的 dummy 文件
RUN echo 'package main; import "fmt"; func main(){fmt.Println("ok")}' > /tmp/dummy.go \
    && go build -o /tmp/dummy /tmp/dummy.go \
    && rm -f /tmp/dummy /tmp/dummy.go
