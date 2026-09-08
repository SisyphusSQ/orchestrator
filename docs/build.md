# Building and testing

当前命令入口：服务端 `orchestrator server`，独立 Go HTTP 客户端 `orch`。客户端构建使用 `make cli`，完整构建使用 `make build`；详见 [客户端说明](orch.md)。旧直连 CLI 与 Shell 客户端不再提供。

Developers have multiple ways to build and test `orchestrator`.

- Using GitHub's CI, no development environment needed
- Using Docker
- Build locally on dev machine

## 源码布局

项目使用根目录的 `go.mod` 和 `go.sum` 管理服务端 Go 依赖，`tools/orch-cli/go.mod` 独立管理客户端依赖：

- `cmd/orchestrator/`：服务启动与本地维护入口；远程业务命令位于独立模块 `tools/orch-cli/`，只通过 HTTP 访问服务端。
- `internal/`：应用内部包，保留现有业务包边界。
- `internal/golib/`：项目维护的日志和辅助实现，包含在根模块测试中。
- `conf/`、`resources/`、`etc/`：配置示例、运行资源和服务安装文件。
- `script/`、`tests/`、`docker/`：兼容脚本、集成与系统测试、容器构建定义。

旧 `go/` 源码目录及 `go/golib` 子模块已移除。开发脚本应使用
`./cmd/orchestrator` 构建入口；`make test-unit` 统一执行 `go test -mod=readonly ./...`，
包含原 golib 的测试，并额外运行嵌套 CLI 模块的测试。`make fmt-check` 覆盖 `cmd/`、`internal/`、`tests/cli/` 和 `tools/orch-cli/`。
原 `github.com/openark/orchestrator/go/*` 包不再提供外部导入兼容性；
`internal/` 下的实现仅供本项目使用。服务端配置搜索路径与运行资源路径保持不变；客户端产物为独立 `orch`。

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
    bin/orchestrator --debug server
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

### 独立客户端产物

`make cli-platforms` 输出 Linux/macOS/Windows 的 amd64/arm64 二进制，均不使用 CGO。服务端发布包只包含服务端与运行资源；独立 `orch` 包只安装 `/usr/bin/orch`。`build.sh -N` 要求已有两个正确目标的二进制；`RELEASE_BASE_PATH` 可以指定隔离的打包输出目录，默认 `/tmp/orchestrator-release`。本地验证不等于已经发布。
