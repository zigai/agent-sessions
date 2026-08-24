# agent-sessions

Track coding-agent sessions, activity, processes, and terminal-multiplexer
locations through a local realtime broker.

Supported harnesses: `claude`, `codex`, `cursor`, `copilot`, `cline`,
`kimi-code`, `grok`, `goose`, `pi`, `omp`, `opencode`, `agy`,
`kilo`, `droid`, `openclaw`, and `hermes`.

## Installation

```sh
go install github.com/zigai/agent-sessions/v2@latest
```

Prebuilt archives and Linux packages are available from
[GitHub Releases](https://github.com/zigai/agent-sessions/releases/latest).

## Quick start

Connect one or more agents and start background tracking:

```sh
agent-sessions setup claude codex
```

View sessions:

```sh
agent-sessions list
agent-sessions watch
agent-sessions show <session>
```

`setup` installs the agent integrations and starts the broker. `list`, `show`,
and `watch` are standalone clients of that broker; `watch --json` provides a
machine-readable transition stream for other tools. The broker keeps effective
state in memory and atomically checkpoints it to the registry file for recovery.
The registry represents current local state. Ended sessions are retained only
as five-minute tombstones to reject late lifecycle reports, then removed
automatically on the next registry update. Use `agent-sessions registry clean
--all` to compact existing state immediately.


Check the installation:

```sh
agent-sessions doctor
```

Run `agent-sessions --help` or `agent-sessions <command> --help` for all
commands and options.

## Hook Installation

```sh
agent-sessions integrations install <harness>
agent-sessions integrations install all
agent-sessions integrations install codex --dry-run --show-content
```

`<harness>` is a supported harness name from the list above.
Use `--show-content` to print generated hook or plugin content; otherwise the
install command prints a concise summary.

## Full Usage

```text
agent-sessions --help
```

```text
Track local coding-agent sessions and where they are running

Usage:
  agent-sessions [flags]
  agent-sessions [command]

Sessions:
  detect        Evaluate an agent detection manifest against saved screen text
  explain       Explain how an agent activity state was selected
  list          Show known sessions
  show          Show session details
  stop          Gracefully stop sessions
  watch         Stream session changes

Setup:
  hook          Run a request/response hook for a harness
  integrations  Install, remove, and inspect agent integrations
  setup         Connect agents and start background tracking

System:
  doctor        Check whether agent-sessions is set up and working
  monitor       Manage background process tracking
  registry      Inspect or clean registry storage

Additional Commands:
  help          Help about any command

Flags:
  -h, --help           help for agent-sessions
      --json           emit JSON (JSON Lines for streams)
      --store string   registry state file path
  -v, --version        print version

Use "agent-sessions [command] --help" for more information about a command.
```

## License

[MIT](https://github.com/zigai/agent-sessions/blob/master/LICENSE)
