# Project rules

- This is a public Codex plugin for Windows, macOS, and Linux, on x86-64 and ARM64.
- Ship self-contained executables. Do not require users to install Python, Node.js, Go, or another runtime.
- Do not add executable script files, installers, or migration scripts to the repository. When asked to give a script, write it in chat only.
- Install through Codex's GitHub marketplace commands. Do not require a manual clone or local build.
- Keep the README short and clear. Put technical details in docs/reference.md and contributor build instructions in CONTRIBUTING.md.
- Tests cover core pricing, usage accounting, and user-visible title behavior only.
- Rebuild all bundled binaries when runtime source or embedded rates change.
- Keep remote main history at exactly one commit. Amend the root commit and push with --force-with-lease after checking the expected remote tip. Never overwrite unexpected remote changes.
- Use clear, short English when communicating with Alex.
