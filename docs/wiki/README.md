# GitHub Wiki source / GitHub Wiki 源文件

This directory is the reviewable source for all current project documentation published to GitHub Wiki. Every published file is listed in `managed-pages.txt`. GitHub Wiki is a delivery mirror, not a second source of truth.

本目录是发布到 GitHub Wiki 的全部当前项目文档的可审查源文件。所有发布文件都列在 `managed-pages.txt` 中。GitHub Wiki 是交付镜像，不是第二个事实源。

## Layout

- `Home.md`, `_Sidebar.md`, and `_Footer.md` are GitHub Wiki special pages.
- `EN-<Topic>.md` and `ZH-<Topic>.md` are one-to-one language pairs.
- Page links use the deployed Wiki URL so they work both in this repository and after publication.
- Executable metadata schema assets stay under `docs/schema/`; issue-specific evidence stays under `docs/verification/` and is not published.
- Superseded upstream pages are retained by Git history instead of a second archive directory.

## Edit and validate

1. Change both language variants in the same pull request.
2. Add or remove both filenames in `managed-pages.txt`.
3. Update `Home.md` and `_Sidebar.md` when navigation changes.
4. Link durable source artifacts such as `conf/`, `internal/`, `resources/`, and `docs/schema/` instead of creating duplicate reference pages outside this directory.
5. Run `make test-docs` before committing.

The validator checks the manifest, language pairs, counterpart links, local and repository targets, Wiki navigation reachability, maintained directory boundaries, and accidental reintroduction of legacy top-level documentation.

## Publish

The Wiki must contain an initial page before its Git repository exists. After that one-time initialization, publish a clean committed source revision with:

```sh
script/publish-wiki
```

The script clones the Wiki into a temporary directory, updates only current and previously managed paths, records the source revision, and pushes normally. Unknown Wiki pages are preserved. Set `WIKI_REMOTE_URL` only when the URL derived from `origin` is unsuitable. No force push is used. If source files are unchanged, it exits without creating an empty Wiki commit.
