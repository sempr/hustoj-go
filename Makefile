MODULE := github.com/sempr/hustoj-go

VERSION := $(shell git describe --tags --always --dirty)
DIRTY   := $(shell git diff --quiet || echo "-dirty")
COMMIT  := $(shell git rev-parse --short HEAD)$(DIRTY)
DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -X '$(MODULE)/cmd.Version=$(VERSION)' \
           -X '$(MODULE)/cmd.Commit=$(COMMIT)' \
           -X '$(MODULE)/cmd.Date=$(DATE)'

PREFIX ?= /usr/bin
SYSDIR ?= /etc/systemd/system
JUDGEHOME ?= /home/judge

build:
	go mod tidy && $(MAKE) -C tini static && \
		go build -tags embed_tini -trimpath -ldflags "$(LDFLAGS)" .

# 离线部署包：交叉编译 amd64/arm64，目标机只需 bash。
dist:
	cd extra && bash package.sh

dist-amd64:
	cd extra && bash package.sh linux/amd64

dist-arm64:
	cd extra && bash package.sh linux/arm64

langs:
	cd extra && bash prepare_langs.sh

check-versions:
	cd extra && bash check_versions.sh

disable-old-judged:
	cd extra && bash disable_old_judged.sh

install: build
	install -m 0755 hustoj $(PREFIX)/hustoj
	install -m 0644 extra/judged-go.service $(SYSDIR)/judged-go.service
	$(MAKE) langs
	install -d $(JUDGEHOME)/etc
	rm -rf $(JUDGEHOME)/etc/langs
	cp -r extra/etc/langs $(JUDGEHOME)/etc/
	$(PREFIX)/hustoj install-tini
	$(MAKE) disable-old-judged
	systemctl enable --now judged-go
