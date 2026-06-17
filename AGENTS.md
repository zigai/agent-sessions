# Repository Instructions

## Harness Adapter Requirements

Every harness integration must be implemented from the harness's current native
extension, hook, or plugin documentation. Before adding or changing a harness:

- Find and read the authoritative docs for that harness's lifecycle hooks,
  plugin system, extension API, event payloads, install paths, and update
  behavior.
- Prefer the harness's native integration surface over wrappers, shims, log
  scraping, or process scanning whenever a native surface exists.
- Use wrappers, PATH shims, tmux scans, or process inspection only as explicit
  fallback behavior, and document the limitations in code/tests/README.
- Capture the harness-native session identity, resume identity/path, cwd,
  lifecycle state, permission/input waiting state, and tmux context when the
  native API exposes them.
- Keep generated hook/plugin/extension files managed, versioned, and
  idempotently updatable so reinstalling hooks replaces stale generated code
  instead of duplicating it.
- Add tests for the installed hook/plugin shape and for the state transitions
  expected from the documented lifecycle events.
- Do not edit the "Hook Installation" section in the readme when adding new harnesses.
