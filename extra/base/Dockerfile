FROM debian:13-slim

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends gcc g++ rustc && rm -rf /var/lib/apt/lists/*
