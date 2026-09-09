# 安全

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Security) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

`orchestrator` 能修改生产复制拓扑。HTTP listener 与 Raft 端口应位于受控网络，对不可信路径使用带认证的 TLS，并按实际启用的操作授予数据库权限。

## HTTP 认证

支持的策略包括：

- `basic`：一个配置的 HTTP 用户名/密码。
- `multi`：配置的高权限凭据，以及只读用户。
- `proxy`：信任认证反向代理写入的身份头；`PowerAuthUsers` 控制写权限。
- `token`：由本地管理命令签发 access token。
- 空认证方式：不做认证，只应在有充分网络隔离的环境使用。
- `ReadOnly: true`：无论认证方式如何，都禁止 HTTP/Web 写操作。

proxy 模式必须在认证前删除客户端传入的同名可信身份头。不要暴露允许客户端任意伪造身份头的 listener。

## TLS 与 mTLS

`UseSSL`、`SSLPrivateKeyFile`、`SSLCertFile` 和可选 `SSLCAFile` 用于保护 Web/API。`UseMutualTLS` 与 `SSLValidOUs` 要求并过滤客户端证书。独立 `orch` 支持可信 CA 以及成对的客户端证书/私钥，且不提供跳过证书校验的选项。

拓扑 MySQL、元数据库 MySQL、可选 Agent 接口和 Consul 分别有自己的 TLS 设置。除非是有记录、限时的迁移窗口，不应使用 skip-verify。证书主机名、CA、有效期和文件访问权限必须从真实服务运行环境验证。

## 密钥与权限

- 配置文件和私钥只允许服务账号读取。
- 密码应通过凭据文件、环境注入或密钥管理器提供，避免出现在命令行。
- 不要把 token、密码或 OTLP 凭据写入 URL、日志、指标标签、traces、Wiki 或 Issue 附件。
- 拓扑账号只授予发现所需读取权限，以及已启用变更/恢复功能确实需要的额外权限。
- 敏感备份需要加密，并单独验证恢复权限。

认证只保护入口，不能代替 Raft 多数派、MySQL 授权、恢复过滤、fencing、审计保留或浏览器来源检查。

## 数据库、Agent 与 Consul 传输

拓扑 MySQL 和元数据库 MySQL 使用相互独立的 TLS 设置与信任材料。必须验证主机名和 CA 链，不能认为 Web listener 证书同时保护数据库流量。拓扑账号只需要发现权限及策略实际启用的变更/恢复权限；元数据库账号只拥有本节点的 metadata schema。

可选 Agent listener 有独立的暴露和超时边界。仅在需要 Agent/seed 流程时启用，限制网络路径，并与标准 Web/API listener 分开做真实 endpoint 验证。

Consul HTTPS 默认校验证书。使用 `ConsulTLSCAFile` 或 `ConsulTLSCAPath` 提供 CA；地址与证书名不同时设置 `ConsulTLSServerName`；mTLS 的客户端证书和私钥必须成对配置。除有记录且限时的迁移窗口外，保持 `ConsulTLSSkipVerify` 为 false。ACL token 通过 `X-Consul-Token` 传递，不进入 URL。

## 部署检查清单

- Web/API 与 Raft listener 只绑定必要网络；Raft 仅对集群成员开放。
- TLS 终止组件必须有明确的信任和身份头行为；通过真实代理验证 `URLPrefix`、重定向、轮询和深链。
- 使用部署配置验证 Basic/multi/proxy/token、只读限制、origin 拒绝、Follower 代理和客户端证书 OU 拒绝。
- 对不会 reload 连接池或 exporter 的组件，凭据和证书轮换必须包含明确重启计划。
- 备份保持加密，并将恢复权限与备份创建分开验证。
