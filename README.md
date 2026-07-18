# agent-sessions

Track coding-agent sessions, activity, processes, and tmux locations on one machine.

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

Check the installation:

```sh
agent-sessions doctor
```

### OpenClaw

The OpenClaw integration installs a managed native plugin with
`openclaw plugins install --link`. It reports only live session identity,
lifecycle, activity, workspace, and resume metadata; it does not import
persisted session history or conversation content. OpenClaw requires the
plugin's scoped `allowConversationAccess` permission so its documented
`before_agent_run` and `agent_end` hooks can run. Follow OpenClaw's restart
notice when a Gateway is already running. Permission/input-waiting state is not
reported because OpenClaw's current typed plugin API does not expose a general
approval-request observer hook.

### Hermes

The Hermes integration installs a managed native Python plugin under
`$HERMES_HOME/plugins` (default `~/.hermes/plugins`) and activates it with
`hermes plugins enable`. It reports live session identity, lifecycle, cwd,
activity, approval-waiting state, and resume metadata without reading prompts,
conversation history, commands, descriptions, responses, or persisted session
history. Start a new Hermes session after installation so Hermes loads the
plugin. Package-manager-managed Hermes configurations must declare the plugin
through their managed configuration instead of this installer.

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
