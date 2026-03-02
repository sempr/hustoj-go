# HUSTOJ-Go

A high-performance online judge system written in Go, compatible with HUSTOJ database schema.

## Features

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

1. Copy `hustoj-go` binary to `/usr/bin/`
2. Copy `extra/judged-go.service` to `/etc/systemd/system/`
3. Set up language rootfs: `cd extra && bash build_rootfs.sh <lang_id>`
4. Copy language configs: `cp -r extra/etc/langs /home/judge/etc/`
5. Start service: `systemctl enable --now judged-go`

## Usage

### Daemon Service

```bash
# Start judge daemon (systemd recommended)
sudo systemctl start judged-go

# Or run directly
hustoj-go daemon --ojhome=/home/judge --debug
```

### Client

```bash
# Judge a single submission
hustoj-go client <solution_id> <runner_id> [oj_home] [DEBUG]
```

### Sandbox

```bash
# Execute command in sandbox environment
hustoj-go sandbox --rootfs=<path> --cmd="<command>" --cwd=<working_dir>
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
├── cmd/              # CLI commands
├── internal/
│   ├── client/        # Judge client implementation
│   ├── daemon/        # Daemon service
│   └── sandbox/       # Sandboxed execution
├── pkg/
│   ├── config/        # Configuration management
│   ├── constants/     # Judge status codes
│   ├── interfaces/    # Core interfaces
│   ├── language/      # Language configuration
│   ├── models/        # Data models
│   └── repository/    # Database operations
└── extra/            # Runtime environments
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
go build -o hustoj-go

# Production build
go build -ldflags="-s -w" -o hustoj-go

# Run tests
go test ./...
```

### Contributing

1. Fork the repository
2. Create a feature branch
3. Follow Go conventions and add tests
4. Submit a pull request

## License

Copyright © 2025 HUSTOJ-Go Contributors

See LICENSE file for details.