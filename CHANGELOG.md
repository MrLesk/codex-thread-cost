# Changelog

## 0.4.0 — 2026-10-15

- Show prices on saved subagent chats, using their nickname when no title exists.
- Include nested and archived subagents and saved forks in parent totals.
- Refresh parent totals when a child finishes through SubagentStop.
- Exclude inherited history from fork costs; verify carried counters against parent records.
- Keep previous prices when any linked chat cannot be priced completely.

## 0.3.3 — 2026-10-11

- Keep the active turn's pricing when speed settings change during a response.
- Include verified legacy usage when newer usage records start at zero after an upgrade.
- Preserve exact custom-rate amounts and round only the displayed price.

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
