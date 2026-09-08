# Documentation / 文档

The current user and operator documentation is maintained bilingually in [`docs/wiki/`](wiki/README.md) and published to the [GitHub Wiki](https://github.com/SisyphusSQ/orchestrator/wiki). The repository copy is the source of truth; do not edit the Wiki as an independent documentation branch.

当前用户与运维文档以中英文双语形式维护在 [`docs/wiki/`](wiki/README.md)，并发布到 [GitHub Wiki](https://github.com/SisyphusSQ/orchestrator/wiki)。仓库内容是唯一事实源，不应把 Wiki 网页维护成另一套文档。

## Current guides / 当前指南

| Topic | English | 中文 |
| --- | --- | --- |
| Overview / 项目概览 | [Overview](wiki/EN-Overview.md) | [项目概览](wiki/ZH-Overview.md) |
| Getting started / 快速开始 | [Getting started](wiki/EN-Getting-Started.md) | [快速开始](wiki/ZH-Getting-Started.md) |
| Configuration / 配置 | [Configuration](wiki/EN-Configuration.md) | [配置](wiki/ZH-Configuration.md) |
| Raft operations / Raft 运维 | [Raft operations](wiki/EN-Raft-Operations.md) | [Raft 运维](wiki/ZH-Raft-Operations.md) |
| CLI | [orch CLI](wiki/EN-orch-CLI.md) | [orch 命令行](wiki/ZH-orch-CLI.md) |
| Web console / Web 控制台 | [Web console](wiki/EN-Web-Console.md) | [Web 控制台](wiki/ZH-Web-Console.md) |
| HTTP API | [HTTP API](wiki/EN-HTTP-API.md) | [HTTP API](wiki/ZH-HTTP-API.md) |
| Failure recovery / 故障恢复 | [Failure recovery](wiki/EN-Failure-Recovery.md) | [故障恢复](wiki/ZH-Failure-Recovery.md) |
| Observability / 可观测性 | [Observability](wiki/EN-Observability.md) | [可观测性](wiki/ZH-Observability.md) |
| Security / 安全 | [Security](wiki/EN-Security.md) | [安全](wiki/ZH-Security.md) |
| Upgrading / 升级 | [Upgrading](wiki/EN-Upgrading.md) | [升级](wiki/ZH-Upgrading.md) |
| Development / 开发 | [Development](wiki/EN-Development.md) | [开发](wiki/ZH-Development.md) |
| Reference / 参考资料 | [Reference](wiki/EN-Reference.md) | [参考资料](wiki/ZH-Reference.md) |

## Detailed reference / 深度参考

The older top-level Markdown files under `docs/` preserve detailed design, historical behavior, configuration notes, and migration evidence. They remain useful references, but they are not the current Wiki navigation contract and may use historical terminology. Start from the bilingual guides above; follow a reference link only when you need the deeper detail.

`docs/` 顶层的既有 Markdown 文件保留了详细设计、历史行为、配置说明和迁移证据。它们仍可作为深度参考，但不再充当当前 Wiki 导航契约，其中可能含有历史术语。请先从上方双语指南进入，只在需要细节时再查阅对应参考页。

- Runtime and deployment: [execution](execution.md), [Raft configuration](configuration-raft.md), [Raft deployment](deployment-raft.md), [high availability](high-availability.md)
- Operations: [failure detection](failure-detection.md), [topology recovery](topology-recovery.md), [status checks](status-checks.md), [observability](observability.md)
- Interfaces: [orch](orch.md), [HTTP API](using-the-web-api.md), [Web console](web.md)
- Configuration: [configuration topics](configuration.md), [sample](configuration-sample.md), [security](security.md), [TLS](ssl-and-tls.md)
- Development: [metadata schema](schema/README.md), [build](build.md), [CI](ci.md), [contributors](developers.md)
- Verification records: [verification index](verification/README.md)

## Maintenance

- Run `make test-docs` while editing. It checks local links, images, Wiki manifests, page reachability, and English/Chinese pairing.
- Run `script/publish-wiki` only after the GitHub Wiki has an initial page and the source change is committed. The script clones the Wiki into a temporary directory, updates only files named by the managed manifest, commits the source revision, and pushes without force.
- Keep commands, configuration fields, routes, and version-sensitive claims grounded in current code. Put one-off validation evidence under `docs/verification/`, not in the Wiki navigation.
