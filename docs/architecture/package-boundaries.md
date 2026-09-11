# 大型 Go package 边界与拆分结果

## 目标

拆包的目标不是平均分散文件，而是让每个 package 只承担一种可描述、可测试的职责，并保持单向依赖。评估同时看生产代码行数、单文件集中度、导出面、隐藏运行时状态和跨层依赖；行数只是发现问题的信号。

`internal/inst`、`internal/inst/change` 与 `internal/logic` 只作为领域命名空间，根目录不得再出现 Go 文件。新职责必须继续落在各自目录下，不平铺到 `internal` 根目录，也不通过 type alias 或双轨 API 维持旧 package。

## 2026-09-10 拆分结果

`internal/inst` 的 14,162 行生产代码已经分布到 20 个真实子 package；根目录生产 Go 文件数为 0。`internal/http` 的业务实现也已经迁入 13 个真实子 package，根目录只保留路由组合与 Web action guard。以下是仍然较大的直接 package，不含测试：

| package | 生产行数 | 文件数 | 当前职责 |
| --- | ---: | ---: | --- |
| `internal/inst/change/relocation` | 2,250 | 2 | move、repoint、pseudo-GTID 对齐和 relocate 算法 |
| `internal/inst/inventory` | 1,882 | 5 | metadata 实例清单、缓存、写缓冲和 pseudo-GTID 状态 |
| `internal/inst/instance` | 1,613 | 9 | `Instance`、`InstanceKey`、坐标、复制状态和模型行为 |
| `internal/inst/change/replication` | 1,595 | 1 | 启停复制、状态切换和底层拓扑命令 |
| `internal/inst/discovery` | 1,213 | 3 | 单实例拓扑探测、dead filter 和 group replication |
| `internal/inst/binlog` | 1,058 | 2 | binlog 事件读取、坐标查找和相关性查询 |
| `internal/inst/change/regroup` | 632 | 1 | 候选排序与 GTID、Pseudo-GTID、binlog server regroup |

本阶段完成的边界如下：

- `inst/instance`：规范实例、键、坐标、复制状态和模型行为。
- `inst/resolve`：hostname、地址、默认端口解析和解析缓存。
- `inst/inventory`：持久化实例清单、查询、缓存与写缓冲；不反向依赖 discovery 或 change。
- `inst/discovery`：拓扑探测和 group replication 发现。
- `inst/analysis`：复制故障分析模型、计算和历史存取。
- `inst/cluster`、`inst/audit`、`inst/candidate`、`inst/maintenance`、`inst/downtime`、`inst/tag`、`inst/pool`、`inst/equivalence`：各自承载原 `inst` 根 package 中对应的独立业务能力。
- `inst/binlog`：binlog 事件与坐标查询。
- `inst/topology`：拓扑展示和实例关系判断。
- `inst/change/replication`：底层复制状态变更。
- `inst/change/relocation`：实例迁移与坐标对齐。
- `inst/change/regroup`：候选选择与复制拓扑重组。
- `inst/gtid`：纯 GTID 解析与集合运算，不依赖仓库内部 package。
- `inst/mysql`：版本化查询和结果字段词汇；显式覆盖 MySQL 5.6、5.7、8.0/8.4 与 MariaDB legacy replication vocabulary。
- `http/api/{instance,maintenance,topology,cluster,recovery,system}`：按能力承载标准 API handler；根 `http` package 不再承载业务 handler。
- `http/{agent,cli,raft,web,observability}`：独立协议适配器与路由入口；由 app 和根路由显式组装，不使用可变全局 API 单例。
- `http/{contract,presenter,request,authz,transport}`：分别承载响应契约、VO 映射与写出、请求参数解析、授权和底层路由传输。
- `logic/{discovery,recovery,raftstate}`：`logic` 根目录只作为命名空间；Raft command 与 snapshot 统一由 raftstate 应用。

## 依赖方向

```text
app --> http route composition
        |
        +--> http/{web,agent,observability}
        |
        +--> http/api/{instance,maintenance,topology,cluster,recovery,system}
        |          |
        |          +--> http/{authz,contract,presenter,request,raft,transport}
        |
        +--> logic/discovery --> logic/recovery
        |                           |
        |                           +--> inst/change/regroup
        |                                      |
        |                                      v
        |                           inst/change/relocation
        |                                      |
        |                                      v
        |                           inst/change/replication
        |
        +--> logic/raftstate --> inst/* + repository

inst/topology --> inst/discovery + inst/inventory + inst/tag
inst/discovery --> inst/inventory --> repository/metadata
inst/{cluster,audit,candidate,maintenance,downtime,tag,pool,equivalence}
               --> repository/metadata
```

关键约束是 `regroup -> relocation -> replication`，禁止反向导入；`inventory` 不依赖 discovery/change；所有 `inst/*` 都不依赖 app、HTTP 或 logic。

## 后续优先级

### 1. 继续收敛 HTTP capability 内部文件

根 `internal/http` 已只负责 route、synonym、leader proxy 和 Web action guard。`api/topology`、`api/recovery` 仍是较大的单一实现文件；下一步优先按用例阶段拆成同 package 文件，只有形成可独立测试且不会制造反向依赖的稳定能力时才继续创建更深子 package。

### 2. 继续收敛 `logic/recovery`

在 `inst` 服务边界稳定后，将 recovery 内部拆为恢复模型、计划选择、执行与 hook、持久化协调。`logic/discovery` 可以依赖 recovery 的公开入口，recovery 不得反向依赖 discovery。

### 3. 评估 `inst/change` 内部可读性

`relocation` 与 `replication` 仍是大文件，但当前分别对应完整的拓扑迁移算法和底层复制命令。下一步优先按算法阶段拆同 package 文件；只有出现可独立测试、且不会制造反向依赖的稳定能力边界时，再继续创建子 package。

`instance` 与 `inventory` 虽然体量较大，但前者共享同一核心模型，后者共享同一缓存和写入一致性责任；目前不按行数机械拆分。

## 自动化约束

`internal/repository/architecture_test.go` 固化以下边界：

- repository 不反向依赖业务 package；
- `inst`、`inst/change` 与 `logic` 根目录不得重新出现 Go 文件；
- 所有 `inst/*` 不依赖 app、HTTP 或 logic；
- `inventory` 不反向依赖 discovery/change；
- `replication` 不依赖 relocation/regroup，`relocation` 不依赖 regroup；
- `inst/gtid` 与 `inst/mysql` 不依赖仓库内部 package；
- `http/transport` 不依赖 HTTP 业务层、logic、inst 或 repository；
- HTTP 子 package 不得反向导入根 `internal/http`，根目录生产文件仅允许路由组合与 action guard；
- `logic/recovery` 不反向依赖 `logic/discovery`、`logic/raftstate`、HTTP 或 app；
- 仓库内不以 type alias 维持旧 package API。
