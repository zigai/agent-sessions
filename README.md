# agent-sessions
[![Release](https://img.shields.io/github/v/release/zigai/agent-sessions?sort=semver)](https://github.com/zigai/agent-sessions/releases)
![Go version](https://img.shields.io/badge/go-1.25.6+-00ADD8.svg)
[![License](https://img.shields.io/github/license/zigai/agent-sessions.svg)](https://github.com/zigai/agent-sessions/blob/master/LICENSE)

A Go CLI project

## Installation

```sh
go install github.com/zigai/agent-sessions@latest
```

### From Source

```sh
git clone https://github.com/zigai/agent-sessions.git
cd agent-sessions
go install .
```

### Releases

Download prebuilt binaries and packages from [GitHub Releases](https://github.com/zigai/agent-sessions/releases).

## Usage

```sh
agent-sessions --help
```

```sh
agent-sessions --version
```

## Development

### Requirements

* Go 1.25.6
* [just](https://github.com/casey/just)
* [golangci-lint](https://golangci-lint.run/)
* [GoReleaser](https://goreleaser.com/)

### Commands

```sh
just check
just test
just lint
just build
```

```sh
just tidy
just fix
just clean
```

### Release Dry Run

```sh
just release-dry-run
```

Release helpers are available through `just release-patch`, `just release-minor`, and `just release-major`.

## Project Layout

```text
.
├── main.go
├── api/
├── assets/
├── build/
├── cmd/
├── configs/
├── deployments/
├── docs/
├── examples/
├── init/
├── internal/
│   └── cli/
│       ├── root.go
│       └── root_test.go
├── pkg/
├── scripts/
├── test/
├── tools/
├── web/
├── go.mod
├── go.sum
├── Justfile
├── .goreleaser.yaml
├── .golangci.yaml
└── LICENSE
```

## License

[Apache-2.0](https://github.com/zigai/agent-sessions/blob/master/LICENSE)
