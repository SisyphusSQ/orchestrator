# Reference

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Reference) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

The bilingual Wiki is the maintained entry point. The following repository pages provide deeper implementation, configuration, and historical context. They may retain old terminology where the subject is explicitly historical; check current code before copying commands into automation.

## Runtime and deployment

- [Execution](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/execution.md)
- [Raft configuration](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/configuration-raft.md)
- [Raft deployment](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/deployment-raft.md)
- [High availability](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/high-availability.md)
- [Backend configuration](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/configuration-backend.md)

## Interfaces and operations

- [orch detailed guide](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/orch.md)
- [orch command mapping](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/orch-commands.md)
- [Web implementation and Storybook](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/web.md)
- [Failure detection](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/failure-detection.md)
- [Topology recovery](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/topology-recovery.md)
- [Pseudo-GTID](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/pseudo-gtid.md)
- [Tags](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/tags.md)

## Platform and maintenance

- [Observability contract](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/observability.md)
- [Security](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/security.md) and [TLS](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/ssl-and-tls.md)
- [Upgrade ledger](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/upgrading.md)
- [Build](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/build.md), [CI](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/ci.md), and [Web development](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/web.md)
- [Configuration samples](https://github.com/SisyphusSQ/orchestrator/tree/main/conf)
- [Apache License 2.0](https://github.com/SisyphusSQ/orchestrator/blob/main/LICENSE)

When documentation and executable behavior disagree, treat tests and current code as the implementation evidence, then fix the documentation in the same change. Report issues at [SisyphusSQ/orchestrator](https://github.com/SisyphusSQ/orchestrator/issues).
