# agent-sessions

Track coding-agent sessions, activity, processes, and tmux locations on one machine.

Supported harnesses: `claude`, `codex`, `cursor`, `copilot`, `cline`,
`kimi-code`, `grok`, `goose`, `pi`, `omp`, `opencode`, `agy`,
`kilo`, and `droid`.

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

Check the installation:

```sh
agent-sessions doctor
```

Run `agent-sessions --help` or `agent-sessions <command> --help` for all
commands and options.

## Hook Installation

```sh
agent-sessions install-hooks <harness>
agent-sessions install-hooks all
agent-sessions install-hooks codex --dry-run
```

`<harness>` is a supported harness name from the list above.

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
