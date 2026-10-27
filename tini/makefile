# tini：沙箱里的 PID 1 兼 init。
#
# 构建信息（版本号 + tini.c 的 md5）编进二进制，用 `tini --version` 查看。
# 主二进制通过 go:embed 内嵌 tini-static，部署时 `hustoj install-tini` 落盘；
# 内嵌的必须是当前源码编出来的那份，所以 static 目标会自检一次。

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
SRC_MD5 := $(shell md5sum tini.c | cut -d' ' -f1)
CFLAGS_BUILD := -DTINI_VERSION='"$(VERSION)"' -DTINI_SOURCE_MD5='"$(SRC_MD5)"'

all: tini static

tini: tini.c
	gcc $(CFLAGS_BUILD) -o tini tini.c

tini-static: tini.c force
	gcc $(CFLAGS_BUILD) --static -o tini-static tini.c

# 自检：确认静态产物确实是当前 tini.c 编出来的（防止内嵌过期构建）。
static: tini-static
	@out=$$(./tini-static --version); \
	echo "$$out"; \
	case "$$out" in *"$(SRC_MD5)"*) ;; \
	*) echo "错误: tini-static 与当前 tini.c 不匹配（源码 md5 $(SRC_MD5)）" >&2; exit 1;; \
	esac
	@mkdir -p ../pkg/tini && cp tini-static ../pkg/tini/tini-static

.PHONY: all static clean install check force

# 版本号是编译期烙进去的，tini.c 不变时 make 不会重编，会留下旧版本字符串；
# 强制每次重编，保证 --version 里的版本号与仓库一致。
force:
clean:
	rm -f tini tini-static

install: static
	cp tini-static /usr/local/bin/tini

check:
	@./tini-static --version
