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

- JSON 配置和私钥只允许服务账号读取。
- 密码应通过凭据文件、环境注入或密钥管理器提供，避免出现在命令行。
- 不要把 token、密码或 OTLP 凭据写入 URL、日志、指标标签、traces、Wiki 或 Issue 附件。
- 拓扑账号只授予发现所需读取权限，以及已启用变更/恢复功能确实需要的额外权限。
- 敏感备份需要加密，并单独验证恢复权限。

认证只保护入口，不能代替 Raft 多数派、MySQL 授权、恢复过滤、fencing、审计保留或浏览器来源检查。细节见[安全](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/security.md)与 [TLS](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/ssl-and-tls.md)参考。
