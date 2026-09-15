# Grok Compatibility

This context defines the terms used by the CPA plugin that repairs Grok tool-call argument representations before response translation.

## Terms

**Model glob**:
A case-insensitive Go `path.Match` pattern selecting the upstream model names handled by the plugin.
_Avoid_: Provider allowlist, model prefix

**Integral float**:
A JSON number written with a floating-point representation whose mathematical value is an exact integer, such as `120000.0`.
_Avoid_: Any float, truncated number

**Complete tool-call arguments**:
A fully parsed arguments JSON value attached to a completed function-call event or buffered response item.
_Avoid_: Argument delta, partial arguments
