# Page-managed recovery settings

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Recovery-Configuration) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

Recovery decisions and pre/post hooks are managed in the Web console under Recovery Settings, not in the example configuration files. The application provides 23 code-owned policy defaults; the database stores only user-selected global overrides and cluster overrides.

Precedence is cluster override > global override > code default. A cluster override requires an explicit, unique alias in `cluster_alias_override`, which prevents the binding from drifting when topology hostnames change. Saves use revision-based optimistic locking and Raft replication. Each recovery record stores fingerprints of the policy and hook assignment that were selected.

Hooks cover nine phases: after failure detection, before failover, after primary failover, after intermediate-primary failover, after any recovery, after an unsuccessful recovery, before graceful takeover, after graceful takeover, and after Take Master. Cluster phases have three explicit modes:

- `inherit`: use the global assignment;
- `replace`: completely replace the global assignment and run the selected profiles in order;
- `disable`: explicitly run no hooks for the cluster and phase.

Hooks run with the Orchestrator process identity. Every profile has a per-command timeout, failure policy, and audited-output limit. Common password, token, secret, and API-key assignments are redacted before audit. Test Run executes the command on the server, so confirm first that the command is safe and idempotent in that environment.

When authentication is enabled, `ConfigurationAdminUsers` and `ConfigurationAdminGroups` grant the narrower recovery-configuration permission. Empty lists deny configuration writes. Local deployments without authentication permit configuration writes, but the API still requires a Raft leader with quorum.

This is a breaking upgrade. The former YAML/JSON recovery-policy and hook keys are rejected and are not imported automatically. Record required values before upgrading, configure them in the Web console after the new version starts, and read back the effective values.
