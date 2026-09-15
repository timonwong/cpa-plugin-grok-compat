# cpa-plugin-grok-compat

CLIProxyAPI dynamic library plugin for Grok tool-call compatibility with Codex.

Grok can serialize an integral tool argument such as `120000` as `120000.0`. Codex rejects that representation when its execution-side type is an integer. This plugin repairs only complete tool-call arguments that are safe to rewrite.

## Behavior

- Applies during CPA's `response_before_translator` hook.
- Matches the selected upstream model against configured, case-insensitive Go glob patterns.
- Converts an integral JSON number only when its tool schema declares `integer`.
- Also converts `wait.yield_time_ms` when its schema declares `number`, matching Codex's `u64` execution contract.
- Repairs Codex Responses Lite's prose-only `exec` tool source for the known integer fields `session_id`, `yield_time_ms`, and `max_output_tokens` (including namespaced `*__exec` calls).
- Leaves fractions, unknown fields, strings, unsafe integers, delta events, incomplete JSON, and unmatched models unchanged.
- Handles both streaming completion events and buffered Responses payloads.

## Build and test

```sh
make test
make build
```

The library is written to `bin/cpa-plugin-grok-compat.dylib` on macOS, `.so` on Linux, or `.dll` on Windows.

## CPA configuration

Copy the built library into the configured CPA plugin directory, enable plugins, and add:

```yaml
plugins:
  enabled: true
  configs:
    cpa-plugin-grok-compat:
      enabled: true
      priority: 100
      models:
        - grok-*
```

`models` is a YAML string array. If it is omitted, the default is `grok-*`. An empty array disables model matching. Any matching pattern enables the normalizer for that model. Invalid or empty patterns reject a configuration update and keep the previous valid configuration active.

Patterns use Go `path.Match` syntax (`*`, `?`, and character classes) and are matched case-insensitively.

## Module layout

- `go/abi.go`: CPA C ABI and RPC envelope adapter.
- `go/config.go`: validated model matcher and runtime configuration snapshot.
- `go/schema.go`: request tool-schema catalog with a bounded cache for streaming calls.
- `go/normalizer.go`: targeted Responses event/SSE normalization and safe numeric conversion.
