FROM debian:trixie-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        mono-mcs \
        mono-runtime \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /code /tmp

WORKDIR /code

CMD ["/bin/sh"]
