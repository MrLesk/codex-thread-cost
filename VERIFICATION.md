# Verification — 0.3.2

- Core tests cover exact price calculations, recorded speed changes, cumulative usage totals, missing estimates, Unicode titles, prefix/suffix placement, and older saved title state.
- The core suite passes locally on macOS. CI runs the same suite on Windows, macOS, and Linux; see [results](https://github.com/MrLesk/codex-thread-cost/actions/workflows/test.yml).
- Go 1.27.1 produced six self-contained executables: macOS, Linux, and Windows, each for x86-64 and ARM64. Linux binaries are statically linked. No language runtime is required by the installed plugin.
- The native macOS executable successfully priced an actual local chat transcript, including recorded Fast usage.
- A direct `Interrupt` invocation updated a real chat title on macOS in about 0.8 seconds. Delivery from the desktop Stop button has not yet been verified.
- Direct `SessionStart` invocations for startup and resume refreshed a real chat on macOS; compact, clear, and unknown sources were ignored. Automatic delivery on app restart has not yet been verified.
- Full automatic desktop hook execution on Windows has not been observed. Cross-platform compilation and core tests do not verify desktop trust or a live Windows App Server connection.

No installer or migration scripts are included. Automated tests are limited to core business logic.
