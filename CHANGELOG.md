# ChangeLog

## Unreleased

- optimization
  - Replace the local application logger implementation with Uber Zap v1.28.0 while preserving the compatibility API, stderr routing, legacy syslog priorities, and explicit logger shutdown. The default text format is now `time\t[LEVEL]\t[caller]\tmessage`; deployments that parse logs must update their patterns before upgrading.
  - Make configured application and audit syslog initialization failures fatal during startup, and make audit file/syslog write failures observable to callers instead of discarding them in per-entry goroutines.
