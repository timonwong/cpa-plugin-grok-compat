# Normalize Complete Tool Arguments Before Translation

The plugin uses CPA's `response_before_translator` capability and rewrites only complete tool-call arguments for configured model globs. It converts integral floats when the tool schema declares an integer, with the explicit `wait.yield_time_ms` compatibility exception. Codex Responses Lite may omit usable schemas in both forms: its built-in `exec` tool is prose, while `exec_command`, `write_stdin`, and `wait` can arrive as direct JSON calls without integer declarations. The compatibility boundary therefore includes only the observed Codex integer fields for those tool names. Fractions, incomplete arguments, and unrelated fields remain unchanged so a representation repair cannot become silent data loss.

## Considered Options

- Rewriting every JSON number was rejected because it would alter legitimate fractional values and unrelated provider fields.
- Request payload mutation was rejected because the response argument representation is produced by Grok and must be repaired after the upstream response exists.
