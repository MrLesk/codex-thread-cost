# Changelog

## 0.3.2 — 2026-10-08

- Refresh the price on session startup and resume to recover missed updates.
- Ignore session starts caused by compaction or clearing the chat.

## 0.3.1 — 2026-10-08

- Update the title on user interruption as well as normal completion.
- Use the three-second timeout supported by Codex interruption hooks.

## 0.3.0 — 2026-10-08

- Replace the script-based implementation with bundled native executables.
- Support Windows, macOS, and Linux on x86-64 and ARM64 without a language runtime.
- Install directly through Codex's GitHub marketplace commands.
- Keep prefix/suffix settings, exact pricing arithmetic, speed changes, cumulative-total checks, and user-written title preservation.
- Leave existing titles and prices unchanged when an estimate is unavailable.
