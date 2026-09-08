# Download and build

Current source and future release artifacts are maintained in [SisyphusSQ/orchestrator](https://github.com/SisyphusSQ/orchestrator). Published artifacts, when available, appear on this repository's [Releases page](https://github.com/SisyphusSQ/orchestrator/releases). Do not infer a release from changes that exist only on `main`.

The server and client are separate binaries:

- `make build` builds the embedded-Web server as `bin/orchestrator` and the HTTP client as `bin/orch`.
- `make binary` builds only the server.
- `make cli` builds only the client.
- `make cli-platforms` builds Linux, macOS, and Windows client archives for amd64/arm64 staging.
- `make package` builds RPM, DEB, and TGZ artifacts into `PACKAGES_PATH`; it does not publish them.

The root Go module does not build the nested `tools/orch-cli` module through `go build ./...`. Use the Make targets so version metadata and embedded Web assets are prepared correctly.

Release assets from the original openark project, Percona's fork, or historical package repositories do not represent this fork's current Raft-only server and standalone client. See [building](build.md), [installation](install.md), and [upgrading](upgrading.md) before deployment.
