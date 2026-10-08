# Thread Cost

Show the estimated cost of a Codex chat in its title:

```text
Fix login bug [~$1.24]
```

Updates when a turn finishes or you stop it. Refreshes the price when a session starts or resumes. Bundled executables support **Windows, macOS, and Linux**, on Intel/AMD and ARM64. No Python or other runtime installation is needed.

## Install

With Codex CLI 0.160.1 or newer and Git installed, run these commands in your terminal (PowerShell on Windows):

```text
codex plugin marketplace add MrLesk/codex-thread-cost
codex plugin add thread-cost@thread-cost-local
```

Restart Codex. Open **Hooks** settings (or `/hooks` in Codex CLI) and trust **Thread Cost**. Finish a turn in a named local chat to see the price.

## Put the price first

Set `position` to `prefix` in the settings file:

- Windows: `%USERPROFILE%\.codex\thread-cost\config.json`
- macOS / Linux: `~/.codex/thread-cost/config.json`

Create the file if needed:

```json
{"position": "prefix"}
```

If it already exists, change only `position`. Use `suffix` to put the price last. Changes apply on the next turn.

## Update

```text
codex plugin marketplace upgrade thread-cost-local
codex plugin add thread-cost@thread-cost-local
```

Restart Codex and review the hook if prompted.

## Remove

```text
codex plugin remove thread-cost@thread-cost-local
```

Restart Codex. Your settings and existing price labels are kept.

## About the estimate

Prices are USD estimates using published API rates, not your ChatGPT bill. Separate subagent chats and tool fees are excluded. If an estimate is unavailable, the title and any old price stay unchanged. The plugin makes no extra model calls.

Supports local Codex chats. Ordinary ChatGPT chats and cloud chats are unsupported.

[Technical details](docs/reference.md) · [Build and contribute](CONTRIBUTING.md) · MIT license
