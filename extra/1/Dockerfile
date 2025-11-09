FROM debian:13-slim

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends gcc g++ && rm -rf /var/lib/apt/lists/*
RUN g++ -x c++-header -fno-asm -Wall -O2 -std=c++14 -DONLINE_JUDGE `find /usr/include/ -name "stdc++.h"`
