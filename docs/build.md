# Building and testing

Developers have multiple ways to build and test `orchestrator`.

- Using GitHub's CI, no development environment needed
- Using Docker
- Build locally on dev machine

## 源码布局

项目使用根目录的 `go.mod` 和 `go.sum` 管理全部 Go 依赖：

- `cmd/orchestrator/`：可执行入口、Cobra 命令树、CLI 参数、命令目录、帮助、业务命令分发及相关测试；HTTP 子命令调用 `internal/app` 的服务启动能力。
- `internal/`：应用内部包，保留现有业务包边界。
- `internal/golib/`：项目维护的日志和辅助实现，包含在根模块测试中。
- `conf/`、`resources/`、`etc/`：配置示例、运行资源和服务安装文件。
- `script/`、`tests/`、`docker/`：兼容脚本、集成与系统测试、容器构建定义。

旧 `go/` 源码目录及 `go/golib` 子模块已移除。开发脚本应使用
`./cmd/orchestrator` 构建入口；`make test-unit` 统一执行 `go test -mod=readonly ./...`，
包含原 golib 的测试。`make fmt-check` 覆盖 `cmd/` 和 `internal/`。
原 `github.com/openark/orchestrator/go/*` 包不再提供外部导入兼容性；
`internal/` 下的实现仅供本项目使用。二进制名称、配置搜索路径与运行资源路径保持不变。

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
go build -mod=readonly -o bin/orchestrator ./cmd/orchestrator
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
