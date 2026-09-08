# Download

当前命令入口：服务端 `orchestrator server`，独立 Go HTTP 客户端 `orch`。客户端构建使用 `make cli`，完整构建使用 `make build`；详见 [客户端说明](orch.md)。旧直连 CLI 与 Shell 客户端不再提供。

`orchestrator` is released as open source and is available at [GitHub](https://github.com/openark/orchestrator).
Find official releases in https://github.com/openark/orchestrator/releases

`orchestrator` packages can be found in https://packagecloud.io/github/orchestrator

源码构建使用 `make build`（服务端及客户端）或 `make cli`（仅客户端）。独立模块位于 `tools/orch-cli`，根模块的 `go get ./...` 不会构建或安装它。

发布产物分为服务端 `orchestrator` 和独立客户端 `orch`；CLI 跨平台文件可由 `make cli-platforms` 生成。具体名称与目录见 [构建说明](build.md)。本仓库当前改造不表示上游下载站已发布同名新产物。

See [Orchestrator for developers](developers.md)
