# Development

Users install the bundled executables through Codex. Only contributors need Go.

Run the core tests:

```text
go test ./src
```

Build for your current computer:

```text
go build -trimpath -ldflags="-s -w" -o thread-cost ./src
```

The binaries in `plugins/thread-cost/bin/` are built from `src/` with Go 1.27, `CGO_ENABLED=0`, `-trimpath`, and `-ldflags="-s -w"`. Set `GOOS` and `GOARCH` to build each supported target:

| Go target | Package directory |
| --- | --- |
| darwin/arm64 | Darwin-arm64 |
| darwin/amd64 | Darwin-x86_64 |
| linux/arm64 | Linux-aarch64 |
| linux/amd64 | Linux-x86_64 |
| windows/arm64 | Windows-ARM64 |
| windows/amd64 | Windows-AMD64 |

Name the executable `thread-cost`, or `thread-cost.exe` on Windows. Rebuild the binaries whenever runtime code or embedded rates change. Keep the remote branch at one root commit by amending and pushing with `--force-with-lease` after checking the expected remote tip.

Automated tests cover core pricing, usage accounting, and title behavior only.
