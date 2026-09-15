# Normalize Complete Tool Arguments Before Translation

The plugin uses CPA's `response_before_translator` capability and rewrites only complete tool-call arguments for configured model globs. It converts integral floats when the tool schema declares an integer, with the explicit `wait.yield_time_ms` compatibility exception. Codex Responses Lite exposes its built-in `exec` tool as prose rather than JSON Schema, so the same boundary includes only the three observed Codex integer fields (`session_id`, `yield_time_ms`, and `max_output_tokens`) in `exec` source text. Fractions, incomplete arguments, and unrelated fields remain unchanged so a representation repair cannot become silent data loss.

## Considered Options

- Rewriting every JSON number was rejected because it would alter legitimate fractional values and unrelated provider fields.
- Request payload mutation was rejected because the response argument representation is produced by Grok and must be repaired after the upstream response exists.
