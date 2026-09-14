# TOO-448 运行时一致性与接口契约验证

- 日期：2026-09-14
- 分支：`suqing/too-448-runtime-snapshots-contracts`
- 基线：`d4039e0d36c2851d2236af254da8d1030157eefa`
- 范围：本地开发工作区；尚无开发提交、PR、发布或部署。
- 环境：Go 1.27.1、Node.js 26.8.1、pnpm 10.33.2，macOS。

## 实现结果

1. 配置通过私有候选副本校验并原子发布；并发重载串行化，失败保留旧配置。Raft 启动规范化也通过副本发布；读取不再修改全局对象。运行时仅 `server.readOnly`、`server.web`、`osc` 可热更新，其他变化明确要求重启。
2. 同步发现的拓扑 I/O 使用请求 Context。异步发现由服务管理并发、期限、审计和关闭，接收后只响应一次，断连不取消任务。延迟检查计时器也由发现调用取消并等待。终止信号通过应用取消与等待路径退出，避免 `os.Exit` 绕过清理。
3. 恢复开始时用单次元数据库事务读取有效策略、Hook 与别名，候选/执行/Hook 使用固定快照。审计指纹来自实际执行内容；反序列化历史不再读取实时策略。recovery 保持同包，按职责拆分文件。
4. `internal/http/contract/spec.json` 集中定义 336 条 HTTP 路由、CLI 命令、Web 响应模型和错误分类；生成 CLI 目录、Web 类型及接口表。真实注册路由、CLI 读写属性和序列化模型共同校验，Web 测试不再正则扫描 Go 源码。修正 nullable successor/Hook commands 和不存在的 Audit.ClusterName 类型。

## 验证证据

| 入口 | 结果 | 范围 |
| --- | --- | --- |
| `make test-unit RACE=1` | PASS | 全部根模块与独立 CLI，包含架构边界、配置 race、隔离 SQLite 快照与本地 Raft 测试 |
| `go test -race ./internal/logic/recovery` | PASS | 保存新 Hook 后实际运行旧/新快照，输出 `oldnew`，执行与审计指纹一致 |
| `go test -race ./internal/inst/discovery ./internal/inst/inventory ./internal/http/api/instance` | PASS | 发现取消、计时器收尾、仅回环 TCP 的异步 HTTP 请求生命周期 |
| `make test-web` | PASS | TypeScript 检查，4 个文件、16 项单元测试 |
| `make build` | PASS | 包含 Web 的服务端及独立 CLI；产物在 `bin/`，不是部署证据 |
| `make test-api-contract` | PASS | 检查生成产物与统一定义一致 |
| `make test-docs` | PASS | 53 个受管 Wiki 页面、索引和链接 |
| `make fmt-check`、`git diff --check` | PASS | 源码格式与差异空白检查 |
| 真实 MySQL 集成 / 部署 E2E / 浏览器验收 | NOT_RUN | 不属于本次真实环境授权；本地隔离测试不替代该证据 |

原始输出保留在本机 `/tmp/too448-*.log`，不提交原始日志。全量测试完成后新增的 Hook/计时器测试分别按相关包执行；不把历史失败结果混称为最终通过。

## 对抗式审查

- 快照是否仍能被隐藏入口修改：发现并修复 Raft Validate/normalize 对已发布对象的写入，以及测试夹具局部赋值错误；架构测试禁止生产调用 TestUpdate。
- 生命周期是否在真实退出时生效：移除发现层直接退出进程，应用先取消、等待 HTTP/手动任务，再关闭 Raft；补齐计时器 Context 与等待。
- Hook 是否可能临时读新版：补齐 TakeMasterHook 上下文，真实命令测试验证执行内容和审计指纹。
- 方法是否能代表读写：CLI 诊断与写接口共享前缀，已按路由检查，保留 GET 写接口兼容性。
- Web 类型是否真实：序列化测试检测 nullable 字段及不存在的字段；历史动态响应明确保留 `legacy`/空 schema，未声称全部历史字段都有完整类型。

## 后续边界

Agent 协议改造、真实 MySQL/部署验收沿用既有任务范围。提交/推送/PR、外部 Wiki 发布和生产操作未执行。新指纹表示有效内容，不是递增行版本；不能用来比较新旧实现的时间顺序。
