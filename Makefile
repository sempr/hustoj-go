MODULE := github.com/sempr/hustoj-go

VERSION := $(shell git describe --tags --always --dirty)
DIRTY   := $(shell git diff --quiet || echo "-dirty")
COMMIT  := $(shell git rev-parse --short HEAD)$(DIRTY)
DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -X '$(MODULE)/cmd.Version=$(VERSION)' \
           -X '$(MODULE)/cmd.Commit=$(COMMIT)' \
           -X '$(MODULE)/cmd.Date=$(DATE)'

build:
	go build -trimpath -ldflags "$(LDFLAGS)" .
