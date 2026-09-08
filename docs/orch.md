# Go HTTP 客户端 orch

`orch` 是独立 Go 客户端，通过 HTTP API 管理 orchestrator；无需服务端二进制、数据库配置、数据库权限、Bash、curl、jq 或 gsed。
服务端与客户端分别构建，客户端模块位于 `tools/orch-cli`，只依赖 Cobra 和 Go 标准库，不导入根模块。

## 构建与运行

```bash
make cli
./bin/orch --endpoint http://127.0.0.1:3000 clusters
./bin/orch topology --cluster my-cluster
./bin/orch discover -i db.example.com:3306
./bin/orch relocate -i replica.example.com:3306 -d primary.example.com:3306
./bin/orch replication-analysis --output json
```

可直接在 `tools/orch-cli` 执行 `go build -o orch .`。`make build` 构建服务端、客户端并同步服务端资源；`make cli-platforms` 生成 Linux/macOS/Windows 的 amd64/arm64 客户端至 `bin/platforms`。
`make test-unit` 覆盖两个模块；`make test-cli` 只测试客户端。根模块的 `go test ./...` 不会覆盖嵌套模块。

## 连接与认证

参数优先于对应环境变量，环境变量优先于默认值。

| 参数 | 环境变量 | 默认值或语义 |
| --- | --- | --- |
| `--endpoint` | `ORCH_ENDPOINT` | `http://localhost:3000`；参数可重复或逗号分隔；环境变量还支持空格分隔 |
| `--user` / `--password` | `ORCH_USER` / `ORCH_PASSWORD` | Basic/multi 认证；密码建议通过环境变量传入 |
| `--token` | `ORCH_TOKEN` | 服务端 token 模式的 `public:secret`，作为 `access-token` Cookie 发送 |
| `--header` | 无 | 可重复的 `Name: value`，用于受信任代理等认证方式 |
| `--ca` | `ORCH_CA` | 追加受信任 CA 的 PEM 文件；默认使用系统证书库 |
| `--cert` / `--key` | `ORCH_CERT` / `ORCH_KEY` | 必须成对提供的 mTLS 客户端证书和私钥 |
| `--timeout` | `ORCH_TIMEOUT` | 每次 HTTP 请求 30s；必须为正数 |
| `--output` | 无 | `text` 或 `json`，默认 `text` |

Go 基线为 1.26.8，第三方依赖固定于各自模块。API 地址可以包含部署前缀，结尾没有 `/api` 时自动追加；不允许地址中嵌入凭据、查询或片段。TLS 默认验证证书，不提供跳过验证开关。
客户端不读取服务端 JSON 配置，也不加载任何 shell profile。旧环境变量不会被读取。

单地址直接使用该服务或代理；多地址先请求 `leader-check`，再尝试 `routed-leader-check`，只在发现阶段切换节点。业务请求使用 HTTP/1.1 短连接只发送一次，避免 HTTP Transport 的连接/流级隐式重试，不自动跟随重定向。
节点本地操作（bootstrap、snapshot、缓存重置）必须只配置一个 endpoint。`which-api` 可查询最终选中的 API 地址。

## 命令与输出

```bash
orch help recover
orch recover --help
orch completion zsh
orch --version
orch which-api
orch api raft/configuration --output json
orch api raft/members --method POST --body '{"id":"node-2","address":"node-2:10008"}'
```

帮助、版本和补全离线可用，不需要正确的连接配置。参数按各命令注册，未提供必填参数、未知参数和多余位置参数在发送请求前失败。
实例支持 `host[:port]`，默认端口 3306；IPv6 使用 `[address]:port`。实例命令可使用逗号/空白分隔的多实例输入，客户端先验证所有参数，再顺序执行，遇错停止。提交池使用专用 `--instances`，必须显式提供；`--instances ""` 清空指定池。
集群命令使用 `--cluster`，也可以用互斥的 `--alias` 或 `-i` 提供集群提示，不隐式猜测本机实例。

普通命令的 JSON 输出是 API Details（或直接响应/成功消息）经过命令投影后的值；通用 `api` 保留原始 JSON envelope。批量成功输出数组，部分失败时先输出已成功项数组，错误写入 stderr。
文本输出将实例键显示为 `host:port`、列表逐行显示、字符串原样显示，复杂结果使用缩进 JSON。新输出不承诺兼容旧脚本。

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | HTTP、认证、服务端业务或输出失败 |
| 2 | 命令参数或连接配置错误 |
| 3 | 变更结果未知，必须先回读服务端状态，不应自动重跑 |
| 4 | 批量命令部分成功后发生明确失败 |

GET 也可能修改拓扑。客户端按命令的业务副作用标记风险；变更请求响应丢失、无效或超时时返回结果未知，不能按 GET 自动重试。通用 `api` 的副作用未知，同样禁止自动重放，并要求单个明确 endpoint。
连接建立失败和 TLS 证书校验失败单独报告，确认尚未发送业务请求时退出 1；发送后的超时/断连仍是结果未知。取消客户端等待不意味着服务端已撤销操作。HTTP 200 的 `Code=ERROR` 也是失败，不能伪装成功。

## 服务端本地管理

```bash
orchestrator server --config /etc/orchestrator.conf.json
orchestrator server --discovery=false --config /etc/orchestrator.conf.json
orchestrator continuous --config /etc/orchestrator.conf.json
orchestrator admin dump-config --config /etc/orchestrator.conf.json
orchestrator admin redeploy-internal-db --config /etc/orchestrator.conf.json
orchestrator admin access-token --owner operator --config /etc/orchestrator.conf.json
```

服务端配置输出可能含凭据，只在受控本地环境使用；不为它提供远程 API。内部恢复选择测试使用隐藏的 `orchestrator admin suggest-promoted-replacement`，不作为远程管理命令。
业务算法、权限、审计及 Raft 协调仍由服务端拥有，客户端不会在 API 不可达时退回直连数据库。

## 一次性切换

本版本删除旧 Shell 客户端及原有本地业务 CLI。只保留新 `orch` 和服务端入口，不保留旧 `-c`、`cli`、单横线长参数、旧别名、旧环境变量或转发脚本。
旧的 `http` 服务启动命令改为 `server`。详见 [升级说明](upgrading.md) 和 [能力映射](orch-commands.md)。
