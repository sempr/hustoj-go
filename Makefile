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

# 数据库迁移相关配置
MIGRATION_DIR := migrations
# 开发环境数据库连接（可被环境变量覆盖）
DB_DSN ?= hustoj:hustoj@tcp(127.0.0.1:3306)/hustoj?parseTime=true
GOOSE := goose -dir $(MIGRATION_DIR) mysql "$(DB_DSN)"

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

# ==================== 数据库迁移目标 ====================

.PHONY: migrate-up migrate-down migrate-status migrate-create migrate-gen sqlc-gen migrate-check migrate-baseline

migrate-up:           ## 执行所有待应用迁移
	$(GOOSE) up

migrate-down:         ## 回滚最后一条迁移（慎用，需迁移文件包含 Down 语句）
	$(GOOSE) down

migrate-status:       ## 查看迁移应用状态
	$(GOOSE) status

migrate-create:       ## 创建新迁移文件：make migrate-create name=add_user_idx
	@[ -n "$(name)" ] || (echo "用法: make migrate-create name=xxx" && exit 1)
	$(GOOSE) create $(name) sql

migrate-gen: sqlc-gen ## 自动生成差异迁移（需安装 atlas: curl -sSf https://atlasgo.sh | sh）
	@command -v atlas >/dev/null 2>&1 || (echo "请先安装 atlas: curl -sSf https://atlasgo.sh | sh" && exit 1)
	@echo "正在生成差异迁移（对比 schema/schema.sql 与开发库）..."
	atlas migrate diff auto --dir file://$(MIGRATION_DIR) \
		--to file://schema/schema.sql \
		--dev-url "mysql://$(DB_DSN)"

sqlc-gen:             ## 重新生成 sqlc 代码（基于 schema.sql 与 queries/）
	sqlc generate

migrate-check:        ## CI 门禁：列级结构校验 + 迁移干运行
	python3 extra/check_schema.py --ignore-comments
	$(GOOSE) -dry-run up 2>&1 | head -20

migrate-baseline:     ## 把已是新结构的库接入版本管理（不执行 SQL，只登记版本）
	$(GOOSE) -baseline up

# 便捷别名
migrate: migrate-up
migrate-dry:          ## 干运行：只打印将执行的迁移
	$(GOOSE) -dry-run up
