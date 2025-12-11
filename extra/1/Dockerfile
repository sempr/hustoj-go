FROM debian:13-slim

ENV DEBIAN_FRONTEND=noninteractive
RUN echo "if you are not in China, please delete this line in Dockerfile" && /usr/bin/sed -i.bak 's|deb.debian.org|mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list.d/debian.sources
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends gcc g++ && rm -rf /var/lib/apt/lists/*
RUN g++ -x c++-header -fno-asm -Wall -O2 -std=c++14 -DONLINE_JUDGE `find /usr/include/ -name "stdc++.h"`
