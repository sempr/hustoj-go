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
	go mod tidy && go build -trimpath -ldflags "$(LDFLAGS)" .

langs:
	cd extra && bash prepare_langs.sh

check-versions:
	cd extra && bash check_versions.sh

disable-old-judged:
	cd extra && bash disable_old_judged.sh

install: build
	install -m 0755 hustoj-go $(PREFIX)/hustoj-go
	install -m 0644 extra/judged-go.service $(SYSDIR)/judged-go.service
	$(MAKE) langs
	install -d $(JUDGEHOME)/etc
	rm -rf $(JUDGEHOME)/etc/langs
	cp -r extra/etc/langs $(JUDGEHOME)/etc/
	$(MAKE) -C tini install
	$(MAKE) disable-old-judged
	systemctl enable --now judged-go
