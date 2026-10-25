# HUSTOJ-Go

A high-performance online judge system written in Go, compatible with HUSTOJ database schema.

The project has two halves:

- **Judge** (`hustoj daemon` / `client` / `judge` / `run`): sandboxed compilation and
  execution, compatible with the HUSTOJ `solution`/`source_code` schema.
- **Web frontend** (`hustoj web`): a Go re-implementation of the HUSTOJ PHP web frontend,
  migrated with a **strangler pattern** — the Go service and the PHP frontend share the same
  MariaDB and judge data directory, and nginx routes traffic by path, so paths can be switched
  one at a time with instant rollback. See `docs/phpweb-go-refactor-design.md`.

## Features

- **Web frontend (PHP → Go)**: same URLs as PHP, switched per-path via nginx, no data sync
- **High Performance**: Concurrent execution of submissions with configurable limits
- **Isolated Execution**: Sandboxed environments for safe code execution
- **Multiple Language Support**: Configurable language environments via TOML files
- **Database Backends**: MySQL and Redis support for job queuing
- **Cross-Platform**: Linux-specific optimizations with fallback for other platforms
- **Special Judges**: Support for custom judges and raw text comparison
- **Docker Support**: Optional containerized execution environments

## Quick Start

### Prerequisites

- Go 1.24 or later
- MySQL database (compatible with HUSTOJ schema)
- Redis (optional, for distributed judging)
- Docker (optional, for containerized execution)
- Linux system with overlay filesystem support (recommended)

### Building

```bash
make
```

### Installation

> 生产环境推荐用**离线部署包**（目标机只需 Bash/systemd，无需 Go/GCC）：
> `make dist` 产出 `dist/hustoj-go-<version>-linux-<arch>.tar.gz`。
> 解压后可直接 `sudo ./install_hustoj_go.sh --local` 一键部署
> （系统依赖 + 数据库 + admin + 示例题库 + 12 种语言 + nginx），
> 或 `sudo ./install.sh` 做分步安装。详见 `docs/deploy-package.md`
> 和 `docs/oneclick-install.md`。

全新目标机可先用包内 `prepare_env.sh` 装好并启动 MariaDB / Redis / Docker
（Debian/Ubuntu + systemd）：`sudo ./prepare_env.sh`，详见
`docs/deploy-package.md`「二、目标机：部署」。

1. Copy `hustoj` binary to `/usr/bin/`
2. Copy `extra/judged-go.service` to `/etc/systemd/system/`
3. Build language rootfs and install configs (needs Docker):
   `cd extra && sudo bash install_langs.sh --ojhome /home/judge`
   （单语言：`bash build_rootfs.sh <lang_id>`，rootfs 落在 `extra/var/rootfs/<id>`）
4. Start service: `systemctl enable --now judged-go`

## Usage

### Web Service

```bash
# Start the web service (systemd recommended; nginx reverse-proxies to it)
hustoj web --ojhome /home/judge [--config <web.toml>] [--debug]
```

Configuration is assembled from two sources (environment variables take precedence):

1. `<ojhome>/etc/web.toml` — operational settings (no credentials)
2. environment variables — injected by systemd `EnvironmentFile=<ojhome>/etc/judge.conf`,
   the source of DB credentials (Go never parses `db_info.inc.php`)

Probes: `GET /healthz` (liveness, no DB), `GET /readyz` (DB connectivity).
The list of paths already served by Go, and the cutover/rollback steps, are in
`docs/web-cutover-checklist.md` and `docs/phpweb-go-refactor-design.md`.

### Daemon Service

```bash
# Start judge daemon (systemd recommended)
sudo systemctl start judged-go

# Or run directly
hustoj daemon --ojhome=/home/judge --debug
```

### Client

```bash
# Judge a single submission (writes results back to the database)
hustoj client <solution_id> <runner_id> [oj_home] [DEBUG]

# Same pipeline but standalone (no database), result to stdout as JSON
hustoj judge --source <file> --lang <id> --data <dir> [--time <ms>] [--memory <kB>] [--spj <n>]
```

### Sandbox

```bash
# Execute a command in the sandbox environment (used internally by the judge)
hustoj run --rootfs=<path> --cmd="<command>" --cwd=<working_dir> [--stdin=...] [--stdout=...]
```

## Configuration

### Database Configuration

Edit `/home/judge/etc/judge.conf`:

```ini
OJ_HOST_NAME=localhost
OJ_USER_NAME=root
OJ_PASSWORD=password
OJ_DB_NAME=hustoj
OJ_PORT_NUMBER=3306
```

### Language Configuration

Language environments are defined in `/home/judge/etc/langs/*.lang.toml`:

```toml
name = "C++"
[fs]
base = "/home/judge/runtime/cpp"
workdir = "/code"

[cmd]
compile = "g++ -Wall -O2 -fno-asm -DONLINE_JUDGE -std=c++11 Main.cpp -o Main"
run = "./Main"
ver = "g++ --version"
env = ["LANG=en_US.UTF-8"]
```

## Architecture

```
├── cmd/              # CLI commands (web / daemon / client / judge / run)
├── internal/
│   ├── web/           # OJ web frontend (PHP → Go migration)
│   ├── client/        # Judge client implementation
│   ├── daemon/        # Daemon service
│   ├── run/           # Sandboxed execution
│   └── shared/        # cgroup helpers
├── pkg/
│   ├── config/        # Judge configuration management
│   ├── constants/     # Judge status codes
│   ├── dbgen/         # sqlc-generated queries (web)
│   ├── interfaces/    # Core interfaces
│   ├── language/      # Language configuration
│   ├── models/        # Data models
│   └── repository/    # Database operations
├── schema/           # schema.sql + sqlc query sources (web)
├── migrations/       # goose migration baseline (00001)
├── phpweb/           # original HUSTOJ PHP frontend (migration reference)
├── docs/             # migration design + cutover checklist
└── extra/            # Runtime environments, gates, systemd units
```

## Development

### Project Structure

- **pkg/**: Reusable packages and interfaces
- **internal/**: Application-specific code
- **cmd/**: Command-line interface
- **extra/**: Runtime configurations and build scripts

### Building from Source

```bash
# Development build
go build -o hustoj

# Production build
go build -ldflags="-s -w" -o hustoj

# Run tests
go test ./...

# Unified quality gate (gofmt/build/vet/staticcheck/tests/php lint/CSRF/
# language index/schema consistency/sqlc codegen sync) — must be green to release
export PATH="$PATH:/root/go/bin"   # sqlc / staticcheck live in ~/go/bin
bash extra/check_all.sh
```

### Contributing

1. Fork the repository
2. Create a feature branch
3. Follow Go conventions and add tests
4. Submit a pull request

## License

Copyright © 2025 HUSTOJ-Go Contributors

See LICENSE file for details.