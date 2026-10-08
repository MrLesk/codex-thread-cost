# Thread Cost

Adds an estimated price to your Codex chat title:

```text
Fix login bug [~$1.24]
```

Updates automatically. Works on **Windows, macOS, and Linux**.

**Side chats (`/side`), cloud chats, and ordinary ChatGPT chats are not supported.**

## Install

You need **Codex CLI 0.160.1 or newer** and **Git** installed.

1. Open Terminal (PowerShell on Windows) and run both commands:

   ```text
   codex plugin marketplace add MrLesk/codex-thread-cost
   codex plugin add thread-cost@thread-cost-local
   ```

2. Restart Codex. Open **Hooks** settings and trust **Thread Cost**. In Codex CLI, type `/hooks`.
3. Give your chat a title and send a message. The price appears when Codex finishes.

## What the price means

An estimated cost in US dollars, **not your ChatGPT bill**. If the cost cannot be calculated, the title stays unchanged and an old price may remain.

[Put the price first](docs/reference.md#put-the-price-first)

## Update

Run both commands, then restart Codex. Trust any new hooks if asked.

```text
codex plugin marketplace upgrade thread-cost-local
codex plugin add thread-cost@thread-cost-local
```

## Remove

Run this command, then restart Codex:

```text
codex plugin remove thread-cost@thread-cost-local
```

Existing price labels and settings are kept.

[Technical details](docs/reference.md) · [Contribute](CONTRIBUTING.md) · [MIT license](LICENSE)
