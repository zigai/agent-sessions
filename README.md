# AHT

AHT (Agent Harness Tracker) tracks coding-agent sessions, activity, processes,
and terminal-multiplexer locations through a local realtime broker.

Supported harnesses: `claude`, `codex`, `cursor`, `copilot`, `cline`,
`kimi-code`, `grok`, `goose`, `pi`, `omp`, `opencode`, `agy`,
`kilo`, `droid`, `openclaw`, and `hermes`.

## Installation

```sh
go install github.com/zigai/aht@latest
```

Prebuilt archives and Linux packages are available from
[GitHub Releases](https://github.com/zigai/aht/releases/latest).

## Quick start

Connect one or more agents and start background tracking:

```sh
aht manage setup claude codex
```

View sessions:

```sh
aht list
aht watch
aht info <session>
aht info <session> --explain
```

`manage setup` installs the agent integrations and starts the broker. `list`, `info`,
and `watch` are standalone clients of that broker; `info --explain` adds the
live activity decision diagnostics, while `watch --json` provides a
machine-readable transition stream for other tools. The broker keeps effective
state in memory and atomically checkpoints it to the registry file for recovery.
The registry represents current local state. Ended sessions are retained only
as five-minute tombstones to reject late lifecycle reports, then removed
automatically on the next registry update. Use `aht manage state
clean --all` to compact existing state immediately.

Check the installation:

```sh
aht manage doctor
```

Run `aht --help` or `aht <command> --help` for all
commands and options.

## Hook Installation

```sh
aht manage integrations install <harness>
aht manage integrations install all
aht manage integrations install codex --dry-run --show-content
```

`<harness>` is a supported harness name from the list above.
Use `--show-content` to print generated hook or plugin content; otherwise the
install command prints a concise summary.

## Full Usage

```text
aht --help
```

```text
Track local coding-agent sessions and where they are running

Usage:
  aht [flags]
  aht [command]

Available Commands:
  list        Show known sessions
  watch       Stream session changes
  info        Show session details and optionally explain activity
  stop        Gracefully stop sessions
  manage      Manage setup, integrations, tracking, and state
  hook        Integration protocol endpoint; not intended for manual use
  help        Help about any command

Flags:
  -h, --help           help for aht
      --json           emit JSON (JSON Lines for streams)
      --store string   registry state file path
  -v, --version        print version

Use "aht [command] --help" for more information about a command.
```

## License

[MIT](https://github.com/zigai/aht/blob/master/LICENSE)
