# agent-sessions

`agent-sessions` is a local registry for coding-agent sessions running on a
machine. Harness hooks, wrappers, and scanners report into one state file so
other tools can answer questions like:

- which agent sessions are open
- which harness owns each session
- whether each session is idle, running, waiting, stale, or exited
- which tmux session, window, and pane the agent belongs to
- which command can resume the harness session

Supported harnesses: `codex`, `pi`, and `opencode`.

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
  gc            Mark old sessions stale and optionally delete stale/exited records
  get           Get one session by registry id
  help          Help about any command
  install-hooks Install harness reporting hooks or shims
  list          List known agent sessions
  path          Print the registry state file path
  report        Upsert a session report from a harness hook or wrapper
  scan          Scan tmux panes for supported harness processes
  summary       Summarize agent counts by tmux session

Flags:
  -h, --help           help for agent-sessions
      --json           emit JSON
      --store string   registry state file path
  -v, --version        print version

Use "agent-sessions [command] --help" for more information about a command
```

## Hook Installation

```sh
agent-sessions install-hooks all
agent-sessions install-hooks codex --dry-run
agent-sessions install-hooks codex
agent-sessions install-hooks pi
agent-sessions install-hooks opencode
```

## Data Model

Each session record stores:
- registry id
- harness: `codex`, `pi`, or `opencode`
- normalized state: `idle`, `running`, `waiting`, `unknown`, `stale`, `exited`
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
