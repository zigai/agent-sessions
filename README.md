# agent-sessions

`agent-sessions` is a local registry for coding-agent sessions on one machine.
It combines documented native harness events with independent runtime evidence:
process presence, tmux location, and current catalog metadata. A native event
can describe what a harness is doing; it never substitutes for process
presence.

Supported harnesses: `claude`, `codex`, `cursor`, `copilot`, `cline`, `kimi-code`, `grok`, `goose`, `pi`, `opencode`, `agy`, `kilo`, and `droid`.

## Installation

`agent-sessions` supports Linux and macOS.

### Go install

```bash
go install github.com/zigai/agent-sessions@latest
```

### Prebuilt binaries

Download release archives and Linux `.deb`/`.rpm` packages from the [GitHub Releases page](https://github.com/zigai/agent-sessions/releases/latest).

### Build from source

```bash
git clone https://github.com/zigai/agent-sessions.git
cd agent-sessions
go build -o agent-sessions .
```

## CLI

```sh
agent-sessions --help
```

Root help groups commands by purpose:

```text
Sessions:
  list              Show known sessions
  watch             Stream session changes
  show <session>     Show session details
  stop <session>     Gracefully stop one session
  stop --all         Gracefully stop every live session

Setup:
  setup <agent...>  Connect agents and start background tracking
  integrations      Install, remove, and inspect agent integrations
  hook              Run a request/response hook for a harness

System:
  monitor           Manage background tracking
  registry          Inspect or clean registry storage
  doctor             Check whether agent-sessions is working
```

The management namespaces provide:

```text
  integrations install/remove/status  Manage agent integrations
  monitor run/enable/disable/status    Manage background tracking
  registry clean/reset/path            Manage registry storage
```

For example, `agent-sessions setup codex claude` installs or updates both
native integrations and idempotently enables the platform monitor. The older
command names remain callable for installed integrations and scripts, but are
hidden from normal help.

`list` supports `--presence` and `--activity` filters. Its table columns are
`ID AGENT SESSION PRESENCE ACTIVITY TMUX WINDOW PANE CWD UPDATED`; rows are
newest first, internal IDs are abbreviated, and `SESSION` shows the most useful
known session label. Summary output keeps presence counts (`TOTAL`, `LIVE`,
`GONE`, `PRESENCE_UNKNOWN`) separate from activity counts (`RUNNING`,
`WAITING`, `IDLE`, `ACTIVITY_UNKNOWN`). Use `watch` for streaming; snapshot-only
and stream-only flags are rejected when used in the wrong mode.

Commands emit human-readable output by default and JSON only when `--json` is
set. Finite commands emit one JSON document. Streaming commands such as
`watch --json` and long-running `monitor run --json` emit one compact JSON
object per line. Managed request/response hooks require `--json` because their
stdout is a harness protocol response.

The foreground observer runs one cycle immediately and then repeats:

```sh
agent-sessions monitor run --once
agent-sessions monitor run --interval 3s --grace-period 0s
agent-sessions monitor run --once --json
agent-sessions monitor run --json
```

With the default zero grace period, a process must be absent from two
consecutive successful inventories before it becomes `gone`. A positive grace
period uses continuous absence duration. Failed or partial inventories never
age a session. The observer writes a health sidecar beside the registry and
uses a per-store lock so two observers cannot reconcile the same store.

`doctor` prints concise `CHECK STATUS MESSAGE` rows for the active setup.
`doctor --verbose` adds every supported integration and the complete capability
matrix. JSON mode returns `ok`, ordered `checks`, and capabilities for the
selected detail level. Diagnostics are evidence-based and do not infer
presence or activity.

## Management commands

```sh
agent-sessions registry reset
agent-sessions registry clean --older-than 168h
agent-sessions registry clean --all
agent-sessions stop --all
agent-sessions stop --all --dry-run
```

Registry cleaning always requires an explicit `--older-than` or `--all`; it
never silently does nothing. `stop` validates current process or tmux evidence
immediately before signaling. It targets only `live` records and skips stale,
missing, reused, or mismatched identities.

The optional managed user service can keep the observer running:

```sh
agent-sessions monitor enable
agent-sessions monitor status
agent-sessions monitor disable
```

Service files are native systemd user units on Linux and launch agents on
macOS. `monitor enable` installs, updates, starts, and safely no-ops when the
desired monitor is already running. Foreign files are never replaced.

## Hook Installation

```sh
agent-sessions install-hooks <harness>
agent-sessions install-hooks all
agent-sessions install-hooks codex --dry-run
```

`<harness>` is a supported harness name from the list above.

## Shim Fallback

Some harnesses do not expose a native interactive session-exit event. For those cases,
`agent-sessions integrations install <agent> --shim --target-binary <real-binary>` can install
a PATH shim that reports `process.start` and `process.exit` around the real command.
Put the generated shim directory before the real harness binary in `PATH`. The shim is a
fallback for process lifetime only; native hooks remain the source of truth for turn,
permission, and idle/running state changes.

## Data Model

The store envelope and every returned session use `schema_version: 2`. Existing
schema-v1 stores are rejected; run `manage reset --store <path>` or move/remove
the old file explicitly.

Each session stores:

- `presence`: `live`, `gone`, or `unknown`, reduced from process evidence and
  terminal native lifecycle events
- nullable `activity`: `running`, `waiting`, `idle`, or `unknown`, reduced only
  from documented native events while the record is not gone
- harness identity, native session id/path, resume argv, cwd, and project root
- complete process identity (`pid` plus boot-qualified start identity)
- tmux location evidence and source-keyed `observations` for native, process,
  tmux, and catalog evidence
- `created_at`, `updated_at`, `presence_changed_at`, and
  `activity_changed_at`

Process arguments are transient discovery data and are never persisted.
Historical transcript rows enrich an already correlated record only. Only
Claude's documented current active-agent catalog may create a catalog-only
record, which starts as `unknown` presence and `unknown` activity.

## License

[MIT](https://github.com/zigai/agent-sessions/blob/master/LICENSE)
