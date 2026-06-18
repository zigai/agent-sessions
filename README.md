# agent-sessions

`agent-sessions` is a local registry for coding-agent sessions running on a
machine. Harness hooks, wrappers, and scanners report into one state file so
other tools can answer questions like:

- which agent sessions are open
- which harness owns each session
- whether each session is idle, running, waiting, unknown, or exited
- which tmux session, window, and pane the agent belongs to
- which command can resume the harness session

Supported harnesses: `claude`, `codex`, `cursor`, `kimi-code`, `grok`, `pi`, `opencode`, `agy`, and `kilo`.

## Installation

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

```text
Track local coding-agent sessions across harnesses and tmux panes

Usage:
  agent-sessions [flags]
  agent-sessions [command]

Available Commands:
  gc            Delete old exited session records
  get           Get one session by registry id
  help          Help about any command
  install-hooks Install harness reporting hooks or shims
  list          List known agent sessions
  path          Print the registry state file path
  report        Upsert a session report from a harness hook or wrapper
  scan          Scan tmux panes for supported harness processes

Flags:
  -h, --help           help for agent-sessions
      --json           emit JSON
      --store string   registry state file path
  -v, --version        print version

Use "agent-sessions [command] --help" for more information about a command.
```

Common read-side views:

```sh
agent-sessions list
agent-sessions list --summary
agent-sessions list --watch
agent-sessions list --watch --summary
```

## Hook Installation

```sh
agent-sessions install-hooks <harness>
agent-sessions install-hooks all
agent-sessions install-hooks codex --dry-run
```

`<harness>` is a supported harness name from the list above.

## Data Model

Each session record stores:
- registry id
- harness: `claude`, `codex`, `cursor`, `kimi-code`, `grok`, `pi`, `opencode`, `agy`, or `kilo`
- normalized state: `idle`, `running`, `waiting`, `unknown`, `exited`
- harness session id and/or session path when known
- resume command when a harness adapter can derive one from session id/path
- cwd and project root
- process ids and tty when reported
- tmux session/window/pane ids, names, indexes, pane cwd, pane pid, and pane tty
- source/confidence labels
- last native harness event and event timestamp
- extra attributes and optional raw JSON payload
- created, updated, last-seen, state-changed, and ended timestamps

## License

[MIT](https://github.com/zigai/agent-sessions/blob/master/LICENSE)
