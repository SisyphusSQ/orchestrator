# Building and testing

Developers have multiple ways to build and test `orchestrator`.

- Using GitHub's CI, no development environment needed
- Using Docker
- Build locally on dev machine

## Build and test via GitHub CI

`orchestrator`'s' [CI Build](ci.md) will:

- build
- test (unit, integration)
- upload an artifact: an `orchestrator` binary compatible with Linux `amd64`

The artifact is attached in the build's output, and valid for a couple months per GitHub Actions policy.

This way, a developer only needs to `git checkout/commit/push` and does not require any development environment on their computer. Once CI completes (successfully), the developer may download the binary artifact to test on a Linux environment.

## Build and test via Docker

Requirements: a docker installation.

`orchestrator` provides [various docker builds](docker.md). For developers:

- run `make run` to build and run the `orchestrator` service
- run `make docker-test` to build `orchestrator` and run unit, integration, and documentation tests
- run `make package` to build `orchestrator` and create distribution packages (`.deb/.rpm/.tgz`)
- run `make system` to build and launch a full CI environment which includes a MySQL topology, HAProxy, Consul, consul-template and `orchestrator` running as a service.


## Build and test on dev machine

Requirements:

- `go` development setup matching the version declared in `go.mod`
- `git`
- `gcc` (required to build `SQLite` as part of the `orchestrator` binary)
- Linux, BSD or MacOS

Run:

```
    git clone git@github.com:openark/orchestrator.git
    cd orchestrator
```

### Build

Download and verify the modules declared in `go.mod` and `go.sum`:

```shell
make deps
```

Build via:

```shell
make build
```

The build uses Go Modules directly and does not require a repository-local `GOPATH` or `vendor/` tree.
Run `make help` to list the supported local and containerized workflows. The
legacy commands under `script/` remain compatibility entrypoints and delegate
to the same Make targets.

Alternatively, if you like and if your Go environment is setup, you may run:

```shell
go build -mod=readonly -o bin/orchestrator ./go/cmd/orchestrator
```

### Run

Find artifacts under `bin/` directory and e.g. run:
```
    bin/orchestrator --debug http
```

### Setup backend DB

If running with SQLite backend, no DB setup is needed. The rest of this section assumes you have a MySQL backend.

For `orchestrator` to detect your replication topologies, it must also have an account on each and every topology. At this stage this has to be the
same account (same user, same password) for all topologies. On each of your masters, issue the following:
```
    CREATE USER 'orc_user'@'%' IDENTIFIED BY 'orc_password';
    GRANT SUPER, PROCESS, REPLICATION SLAVE, RELOAD ON *.* TO 'orc_user'@'%';
```
Replace `%` with a specific hostname/`127.0.0.1`/subnet. Choose your password wisely. Edit `orchestrator.conf.json` to match:
```
    "MySQLTopologyUser": "orc_user",
    "MySQLTopologyPassword": "orc_password",
```
