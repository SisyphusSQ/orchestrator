# Executing via command line

日常运维统一使用独立 Go HTTP 客户端 [orch](orch.md)。

```bash
orch --endpoint http://127.0.0.1:3000 clusters
orch topology --cluster my-cluster
orch discover -i db.example.com:3306
orch relocate -i replica.example.com:3306 -d primary.example.com:3306
orch replication-analysis --output json
orch help recover
```

服务端使用 `orchestrator server --config /etc/orchestrator.conf.json` 启动；本地维护放在 `orchestrator admin`。
旧直连业务 CLI 和 Shell 客户端已删除，不再支持 `-c` 或旧参数别名。
全部能力去向见 [命令映射](orch-commands.md)，连接、认证、输出、退出码及构建方式见 [客户端说明](orch.md)。
