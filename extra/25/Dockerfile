FROM debian:13-slim

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
    && apt-get install -y --no-install-recommends rustc \
    && rm -rf /var/lib/apt/lists/*
RUN /usr/bin/ln -sf /usr/bin/rustc /usr/local/bin/rustc
