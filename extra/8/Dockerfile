FROM debian:13-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update     && apt-get install -y --no-install-recommends         perl     && rm -rf /var/lib/apt/lists/*     && mkdir -p /code /tmp

WORKDIR /code

CMD ["/bin/sh"]
