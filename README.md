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

Explain or test state detection:

```sh
agent-sessions explain --pane %12
agent-sessions detect --harness codex --file screen.txt
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

## Agent state reliability

The monitor resolves the foreground process controlling each tmux pane and uses
bounded, live bottom-screen detection for four agents. Raw terminal contents
are evaluated in memory and are never written to the registry.

| Agent | Activity authority |
| --- | --- |
| Codex | Screen manifest; hooks provide session identity and hints |
| Claude Code | Screen manifest; hooks provide session identity and hints |
| OpenCode | Active matching plugin; screen fallback otherwise |
| Pi | Active matching extension; screen fallback otherwise |

Screen manifests classify `running`, `waiting`, or `idle`. If the evidence is
insufficient, the agent has no tmux pane, pane enumeration fails, or capture
fails, activity becomes `unknown`; detection never guesses idle. A complete OpenCode/Pi integration report is tied to the exact
PID and process-start identity, so a fallback capture cannot overwrite it and
a replacement process cannot inherit its state. Integration evidence is active
only when it reports a known activity for that same live process, is not ended,
is not future-dated, and is no more than 30 seconds old; otherwise screen
fallback records the exact reason. This lease recovers if an integration stops
reporting while its process remains alive, while every diagnostic still shows
the report's exact age.

Bundled manifests can be overridden locally:

```text
~/.config/agent-sessions/detection/{codex,claude,opencode,pi}.toml
```

Overrides use schema version `1` and ordered `[[rules]]` with `id`, `state`,
`priority`, and optional `region`, `all`, `any`, `none`, `regex_all`,
`regex_any`, `regex_none`, `title_any`, and `title_regex_any` fields. Regions
are `all`, `top:N`, or `bottom:N`. Overrides are capped at 1 MiB. Invalid
overrides are ignored in favor of the bundled manifest and surfaced by
`explain` and monitor health.

Use `agent-sessions explain <session>` or `--pane <id>` to inspect process
matching, selected authority, hook identity/freshness, the matched manifest
rule, fallback reason, and registry decision. For offline fixture testing use
`agent-sessions detect --harness <agent> --file <path>`; pass `-` to read stdin.
Input is capped at 1 MiB.

On Linux, wrappers can provide a scoped process-discovery hint (known wrapper
argument forms are also resolved on macOS):

```sh
AGENT_SESSIONS_AGENT=claude fence -- claude
```

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
