# orch 命令行

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-orch-CLI) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

`orch` 是远程管理使用的独立 Go 客户端，只访问 HTTP API，不需要服务端配置或数据库权限。

## 构建与连接

```sh
make cli
export ORCH_ENDPOINT="https://orchestrator.example.com"
bin/orch clusters
bin/orch topology --cluster production
bin/orch replication-analysis --output json
```

endpoint 未带 `/api` 时会自动补齐。`--endpoint` 可重复或使用逗号分隔；`ORCH_ENDPOINT` 还支持空格分隔。配置多个 endpoint 时，客户端先探测 Leader 检查，选择 API 地址，然后只发送一次业务请求。bootstrap、snapshot 等节点本地命令必须只配置一个 endpoint。

## 认证与 TLS

| 参数 | 环境变量 | 用途 |
| --- | --- | --- |
| `--user`、`--password` | `ORCH_USER`、`ORCH_PASSWORD` | Basic 或 multi 认证 |
| `--token` | `ORCH_TOKEN` | `public:secret` access-token Cookie |
| `--header` | — | 可重复的可信代理认证头 |
| `--ca` | `ORCH_CA` | 追加可信 CA PEM |
| `--cert`、`--key` | `ORCH_CERT`、`ORCH_KEY` | 成对提供的 mTLS 客户端材料 |
| `--timeout` | `ORCH_TIMEOUT` | 单次请求超时，默认 30 秒 |
| `--output` | — | `text` 或 `json` |

密码与 token 应通过环境变量或密钥管理器提供。客户端始终校验证书，没有跳过 TLS 校验的参数。

## 常用操作

```sh
orch discover --instance db-1.example.com:3306
orch begin-maintenance --instance db-1.example.com:3306 \
  --owner operator --reason planned-change
orch relocate --instance replica.example.com:3306 \
  --destination primary.example.com:3306
orch recover --instance failed-primary.example.com:3306
orch raft-configuration --output json
orch which-api
```

使用 `orch help <command>` 查看当前二进制实际注册的参数。`orch api PATH` 可调用相对 API 路径，但客户端会把副作用视为未知，不能盲目重试。

## 命令目录与迁移

机器可读的 [`catalog.json`](https://github.com/SisyphusSQ/orchestrator/blob/main/tools/orch-cli/internal/cmd/catalog.json) 是生成 HTTP 命令、路径、query 参数、读写分类和输出投影的权威。运维契约以当前二进制 help 为准，不要把静态命令表复制进自动化。

命令组覆盖智能/经典/GTID/Pseudo-GTID 拓扑调整、复制控制、binlog、实例/集群发现、资源池与搜索、标签、maintenance/downtime、恢复与确认、候选规则、主机名解析、全局恢复控制、Agent 命令和 Raft 管理。支持的集群目标使用互斥的 `--cluster`、`--alias` 或 `--instance` 提示；实例未指定端口时才默认使用 3306。

仅本地执行的操作继续由服务端二进制承担：

- `orchestrator server` 启动 Raft、HTTP/Web 和后台发现，除非显式关闭 discovery。
- `orchestrator admin dump-config` 输出本地配置，可能暴露密钥。
- `orchestrator admin redeploy-internal-db` 执行本地元数据库维护。
- `orchestrator admin migrate-metadata-id --config=/absolute/path/orchestrator.yaml` 在停写和备份后将旧元数据库迁移为统一自增 `id` 主键。
- `orchestrator admin access-token --owner ...` 在本地签发 access token。

旧名称不保留别名：`continuous` 和 `http` 改为 `orchestrator server`；历史 `instance`、`downtimed`、detach/reattach 和 Raft election 形式改用 help 中的当前显式命令。no-op、发现行为等进程级服务选项不能通过远程请求修改。

持续时间接受带 `s`、`m`、`h`、`d` 或 `w` 后缀的非负值。owner、reason、destination、pattern 和集群选择都必须显式提供，不再从本机或服务端配置推断。文本输出面向人；自动化应优先使用 `--output json`，且不能从空输出推断成功。

## 失败语义

退出码 0 表示确认成功；1 表示 HTTP、认证、服务端业务或输出失败；2 表示本地参数/配置错误；3 表示写操作结果未知，必须回读；4 表示批量操作部分成功后出现明确失败。HTTP 200 但 API `Code=ERROR` 仍是失败。取消等待或客户端超时不代表服务端已经回滚。
