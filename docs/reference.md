# Technical details

## Runtime

The plugin ships self-contained executables for Windows, macOS, and Linux (x86-64 and ARM64). The Codex hook launches the matching executable directly. No script files or language runtime are part of the installed plugin.

It reads local usage records and uses Codex App Server to update the title. It first tries the running metadata server's proxy, then a short-lived local metadata server. It never starts a model turn.

It handles `Stop`, `Interrupt`, and `SessionStart` events. Session-start updates are limited to `startup` and `resume`; `compact` and `clear` are ignored. This refreshes recorded usage after a missed update, while keeping the title unchanged when an estimate is unavailable. Codex limits interruption hooks to three seconds. Updates use usage already written to the transcript; if time runs out or usage is incomplete, the previous price may remain until a later completed turn. Force-quitting the app or killing its process cannot reliably run a hook.

## Settings

The README lists the settings file for each platform. Available settings:

```json
{
  "enabled": true,
  "position": "suffix",
  "precision": 2,
  "default_tier": "standard",
  "codex_binary": "codex",
  "transport": "auto"
}
```

`precision` accepts 0–6 decimal places. `enabled: false` stops updates. Use a full path for `codex_binary` if the desktop app cannot find it. On Windows, use forward slashes or escape backslashes in JSON.

`CODEX_HOME` overrides the usual `.codex` directory. `THREAD_COST_HOME` overrides this plugin's settings and saved estimates. These are separate from the plugin cache, so updates preserve them.

## Accounting

Rates are bundled from the 2026-10-08 API price snapshot. Supported models: `gpt-6-astra`, `gpt-6.1-sol`, `gpt-6-luna`, and `gpt-5.3-codex`, including dated variants. Other models can be added through a `rates` object in the settings file using the format in `src/rates.json`.

The estimate includes recorded input, cached input, cache writes, and output. Reasoning tokens are already part of output. Long-context rules and recorded speed changes apply per request. Monetary calculations use exact rational arithmetic; displayed prices round half up.

The parser follows thread-owned settings snapshots. `priority` means Fast; an absent or null tier in a full snapshot means Standard. Before a matching snapshot exists, `default_tier` applies. Duplicate responses are not added twice, and cumulative totals must reconcile. Checked legacy token-count records are also supported.

Incomplete histories, unknown rates, invalid records, or failures leave the title unchanged. A previous displayed price can therefore be stale. Separate subagent chats, hosted tool fees, image tools, taxes, regional premiums, discounts, and subscription billing are excluded. Updating rates recomputes the estimate; this is not a historical billing ledger.

Sources: [OpenAI API pricing](https://developers.openai.com/api/docs/pricing), [Astra](https://developers.openai.com/api/docs/models/gpt-6-astra), [Sol](https://developers.openai.com/api/docs/models/gpt-6.1-sol).

## Privacy and limitations

The plugin makes no external network requests or telemetry calls. It stores titles, numeric summaries, and diagnostics locally, not message bodies or credentials. Codex retains its normal network behavior.

Transcript and App Server formats can change. The plugin checks titles before and after updates, but the metadata API cannot make a rename fully atomic. A simultaneous user rename can still race with the hook.

If no price appears, check that the plugin is enabled, its hook is trusted, and `codex --version` works. Restart the app after installing Codex or changing PATH. The plugin needs a named local chat with recorded usage. Managed environments may restrict hooks. See the official [hook documentation](https://learn.chatgpt.com/docs/hooks).
