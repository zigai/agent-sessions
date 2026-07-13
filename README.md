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

The primary commands are:

```text
  doctor         Check registry, observer, queue, and integrations
  gc             Delete old gone session records
  get            Get one session by registry id
  install-hooks  Install harness reporting hooks or shims
  list           List known agent sessions
  manage         Manage registry state and agent sessions
  observe        Reconcile process, tmux, and catalog evidence
  path           Print the registry state file path
  report         Record a native harness observation
  service        Manage the optional native observer service
```

`report [harness]` accepts independent `--presence` and `--activity` values.
The supported presence values are `live`, `gone`, and `unknown`; supported
activity values are `running`, `waiting`, `idle`, and `unknown`. A gone record
always renders activity as JSON `null` and as `-` in list output.

`list` supports `--presence` and `--activity` filters. Its table columns are
`ID HARNESS PRESENCE ACTIVITY TMUX WINDOW PANE CWD UPDATED`. Summary output
keeps presence counts (`TOTAL`, `LIVE`, `GONE`, `PRESENCE_UNKNOWN`) separate
from activity counts (`RUNNING`, `WAITING`, `IDLE`, `ACTIVITY_UNKNOWN`).

The foreground observer runs one cycle immediately and then repeats:

```sh
agent-sessions observe --once
agent-sessions observe --interval 3s --grace-period 0s
agent-sessions observe --once --json
```

With the default zero grace period, a process must be absent from two
consecutive successful inventories before it becomes `gone`. A positive grace
period uses continuous absence duration. Failed or partial inventories never
age a session. The observer writes a health sidecar beside the registry and
uses a per-store lock so two observers cannot reconcile the same store.

`doctor` prints `CHECK STATUS MESSAGE` rows and a capability table. JSON mode
returns `ok`, ordered `checks`, and canonical per-harness `capabilities`.
Diagnostics are evidence-based and do not infer presence or activity.

## Management commands

```sh
agent-sessions manage reset
agent-sessions manage stop-all
agent-sessions manage stop-all --dry-run
```

`manage stop-all` validates current process or tmux evidence immediately before
signaling. It targets only `live` records and skips stale, missing, reused, or
mismatched identities.

The optional managed user service can keep the observer running:

```sh
agent-sessions service install
agent-sessions service update
agent-sessions service status
agent-sessions service uninstall
```

Service files are native systemd user units on Linux and launch agents on
macOS. Foreign files are never replaced.

## Hook Installation

```sh
agent-sessions install-hooks <harness>
agent-sessions install-hooks all
agent-sessions install-hooks codex --dry-run
```

`<harness>` is a supported harness name from the list above.

## Shim Fallback

Some harnesses do not expose a native interactive session-exit event. For those cases,
`agent-sessions install-hooks <harness> --shim --target-binary <real-binary>` can install
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
