# TOO-426 orch CLI 本地验证报告

> 这是特定变更的历史验证记录，不是当前用户指南。当前使用说明见 Wiki 的 [orch CLI](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-orch-CLI)，双语入口见[文档索引](../README.md)。

日期：2026-09-08。分支：`suqing/too-426-http-cli`；基线：`d6fe13d8`。本报告覆盖本地开发工作区，不代表提交、合并、远端 CI、生产验收或发版。

## 实现范围核对

- `tools/orch-cli` 独立 Go 模块，产物 `orch`；依赖仅为标准库及 Cobra/pflag（Windows 补全依赖 mousetrap）。无根模块 replace、服务端 internal 导入、数据库驱动或运行资源。
- 原 Go 127 个目录项：121 个同名远程命令、5 个本地入口、1 个旧别名合并；加上 13 个 Shell 补充命令和 5 个当前 Raft 管理命令，合计 139 个远程命令。另有 `api`、`which-api`、Cobra 帮助/补全。当前能力入口见 Wiki 的 [orch CLI](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-orch-CLI)。
- 服务端新增 18 个薄 HTTP 能力入口及 repoint 无目的地路由；保留业务层，补齐必要参数与鉴权/Raft 错误语义。手动发现不再依赖后台循环初始化。
- 删除 Shell client、profile、原直连 CLI 和兼容参数；同步服务启动、脚本、系统测试、构建/打包与文档。服务端使用 `orchestrator server`，本地维护使用 `admin`。

## 已执行验证

所有最终构建使用 Go 1.26.8；工具链通过命令级 `GOTOOLCHAIN` 选择，没有改全局 Go 配置或安装系统工具。

| 层级 | 入口与结果 |
| --- | --- |
| 模块与格式 | `make deps fmt-check` 通过；两个模块均验证依赖并检查 tidy 差异 |
| 全仓及客户端 race | `make test-unit RACE=1` 通过；后续修正对 `internal/http` 和独立 CLI 重新执行 race 测试通过，其余包复用已有结果 |
| HTTP 合同 | 客户端目录与服务端实际路由对照通过；参数编码、可选目标、维护期限、查询参数、只读拒绝在访问数据库前生效 |
| 客户端异常 | 认证头与 Cookie、默认 TLS 拒绝/自定义 CA、取消/超时、HTTP/API 错误、无效 JSON/Code、部分失败、离线帮助/补全、响应丢失后请求次数均有测试；变更不重放 |
| SQLite 集成 | `bash tests/integration/test.sh sqlite` 的 104 个 fixture 全部通过。每个 fixture 启动实际 HTTP 服务，由独立 orch 调用；临时 SQLite 和进程自动清理 |
| 真实 MySQL 拓扑 | `make test-cli-e2e` 通过；本机已有 MySQL 8.0.46，三个回环临时实例，无既有数据库连接。发现、维护（含 `/`、`+`、`%` 原因和期限）、池提交/清空、标签、GTID relocate、真实复制数据回读、主库停止后的强制故障恢复通过 |
| 真实 Raft 集群 | 同一 E2E 入口启动三个临时服务节点。bootstrap、成员添加、follower 转发、多地址选主、标签新增/单项删除/批量删除的各 SQLite 回读、leader 停止后的写入和无 quorum 拒绝均通过 |
| 跨平台构建 | `make cli-platforms` 通过：Linux/macOS/Windows × amd64/arm64；检查 ELF/Mach-O/PE 及架构。CLI 均 `CGO_ENABLED=0`；macOS arm64 做了实际运行验收 |
| 本机打包 | `build.sh -N -t darwin -a arm64` 使用专用 `RELEASE_BASE_PATH`；客户端 TAR 仅包含 `usr/bin/orch`，服务端 TAR 包含 93 个文件且无旧 Shell client。禁用 macOS AppleDouble 元数据进入 TAR |
| 文档与脚本 | `make test-docs`、`git diff --check` 通过；变更 Bash 脚本语法检查通过，索引、链接和命令示例同步 |
| 漏洞 | `make cve` 覆盖两个模块。Go 1.26.5 原有 7 项服务端可达标准库漏洞促成补丁升级；Go 1.26.8 下服务端可达漏洞为 0，仅有未调用的 x/crypto/openpgp 模块提示 GO-2026-5932；独立 CLI 无漏洞提示。第三方模块版本未升级 |

旧 CLI 的“低版本/更高 SQL_Delay 不可复制”与“标签不存在”曾返回空输出而成功；对应 fixture 改为断言明确业务失败。复制状态统一输出布尔值，空字符串文本不额外输出空行；这属于已确认的一次性输出契约切换。

## 对抗式审查结果

1. **两个入口仍然重复或客户端隐式直连**：旧分发与脚本已删除，客户端模块及二进制依赖检查确认不含服务端/数据库。
2. **GET 变更被隐式重放、Raft 成功后响应丢失被误判**：客户端与 Raft 代理关闭连接复用产生的隐式 GET 重试，不跟随重定向；明确传播 indeterminate，异常测试记录请求次数。
3. **命令名字存在但参数/路由能力丢失**：逐项对照原 Go/Shell 与真实路由，补齐 repoint、flush、strict、维护期限、全部池查询及清空；旧别名逐项列出归并入口。
4. **编码或输出破坏脚本语义**：路由在编码状态匹配、参数只解码一次；特殊字符、空池、布尔输出、整数精度、stderr/stdout 和批量中止有验证。
5. **删 client 后构建、安装或安全边界遗漏**：双模块入口和跨平台产物验证；标签/KV/快照操作补鉴权，标签删除走 Raft；打包不再包含 Shell、profile 或 macOS 资源叉文件。

## Not Run 与边界

- 未执行 Docker 全量系统套件、Linux RPM/DEB 的实际安装及全部旧 MySQL 版本矩阵；已迁移相关入口和脚本，但不将 Bash 语法检查视为系统测试通过。
- Linux/Windows 和 macOS amd64 本次为交叉构建及格式检查，未声称这些平台的运行时验收已完成。
- 未开启或运行远端 CI，未连接生产环境，未 commit/push/合并/发版。
- 新 Raft command 与删除返回契约要求节点使用同一新版本；不提供旧版本混跑或旧 CLI 兼容期。
- CLI 所有变更仍由服务端执行业务逻辑。客户端取消等待或返回结果未知时，应先回读状态；不表示服务端已撤销操作。

使用方式见 Wiki 的 [orch CLI](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-orch-CLI)，切换要求见[升级指南](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Upgrading)。
