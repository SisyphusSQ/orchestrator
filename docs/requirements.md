# Requirements

`orchestrator` is a standalone application. The metadata backend supports MySQL 5.7 through 8.0, TiDB, OceanBase in MySQL mode, and embedded SQLite. MySQL-compatible backends use the conservative common DDL documented in [Metadata schema](schema/README.md); because TiDB and OceanBase versions are not interchangeable, validate the exact target release with the provided empty-database test before deployment. When configured to run with a `SQLite` backend, no further database dependency is required.

`orchestrator` is built and tested on Linux 64bit and Mac OS/X. Official binaries are available for Linux only.
