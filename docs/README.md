# Documentation / 文档

Current user, operator, and developer documentation is maintained bilingually in [`docs/wiki/`](wiki/README.md) and published to the [GitHub Wiki](https://github.com/SisyphusSQ/orchestrator/wiki). The repository copy is the source of truth; the published Wiki is a delivery mirror.

当前用户、运维和开发文档以中英文双语形式维护在 [`docs/wiki/`](wiki/README.md)，并发布到 [GitHub Wiki](https://github.com/SisyphusSQ/orchestrator/wiki)。仓库内容是唯一事实源，远端 Wiki 是发布镜像。

## Maintained assets / 受维护资产

| Path / 路径 | Responsibility / 职责 |
| --- | --- |
| [`wiki/`](wiki/README.md) | Reviewable bilingual Wiki source and publication manifest / 可审查的双语 Wiki 源文件及发布清单 |
| [`architecture/`](architecture/repository-models.md) | Internal dependency boundaries and model ownership / 内部依赖边界与模型归属 |
| [`schema/`](schema/README.md) | Executable metadata schema, compatibility matrix, and migration guide / 可执行元数据库 Schema、兼容矩阵及迁移指南 |
| [`verification/`](verification/README.md) | Issue-specific validation records not published as user documentation / 不作为用户文档发布的 Issue 验证记录 |

Project history, removed documentation, and superseded behavior remain available through Git history. Do not add a second archive of stale pages under `docs/`.

项目历史、已删除文档和被替代的行为可从 Git 历史查询。不要在 `docs/` 下再建立一套长期旧文档归档。

## Maintenance / 维护

- Update both `EN-*.md` and `ZH-*.md` pages when behavior changes.
- Keep `docs/wiki/managed-pages.txt`, `Home.md`, and `_Sidebar.md` synchronized with the published page set.
- Run `make test-docs` during development. It checks links, the Wiki manifest and bilingual pairing, maintained directory boundaries, and the absence of legacy top-level pages.
- Publish a clean committed revision with `script/publish-wiki`. The script updates only managed pages and does not force-push.
- Keep one-off acceptance evidence under `docs/verification/`; keep executable schema contracts under `docs/schema/`.

- 行为变化时同时更新 `EN-*.md` 与 `ZH-*.md`。
- 保持 `docs/wiki/managed-pages.txt`、`Home.md`、`_Sidebar.md` 与发布页面集合一致。
- 开发阶段运行 `make test-docs`，检查链接、Wiki 清单、双语配对、受维护目录边界和遗留顶层页面回归。
- 使用 `script/publish-wiki` 发布干净且已提交的 revision；脚本只更新受管页面且不 force-push。
- 一次性验收证据放入 `docs/verification/`，可执行 Schema 契约放入 `docs/schema/`。
