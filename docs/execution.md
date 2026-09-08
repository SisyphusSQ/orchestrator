# Execution

#### Executing as web/API service

Assuming you've installed `orchestrator` under `/usr/local/orchestrator`:

    cd /usr/local/orchestrator && ./orchestrator server

`Orchestrator` will start listening on port `3000`. Point your browser to `http://your.host:3000/`
After configuring a stable Raft node identity and data directory, bootstrap one seed node and add the other members; see [Raft configuration](configuration-raft.md).

If you like your debug messages, issue:

    cd /usr/local/orchestrator && ./orchestrator --debug server

or, even more detailed in case of error:

    cd /usr/local/orchestrator && ./orchestrator --debug --stack server

The above looks for configuration in `/etc/orchestrator.conf.json`, `conf/orchestrator.conf.json`, `orchestrator.conf.json`, in that order.
Classic is to put configuration in `/etc/orchestrator.conf.json`. Since it contains credentials to your MySQL servers you may wish to limit access to that file.
You may choose to use a different location for the configuration file, in which case execute:

    cd /usr/local/orchestrator && ./orchestrator --debug --config=/path/to/config.file server

Web/API service will, by default, issue a continuous, infinite polling of all known servers. This keeps `orchestrator`'s data up to date.
You typically want this behavior, but you may disable it, pausing background discovery while keeping Raft and API/Web available. Manual discovery still updates instance state:

    cd /usr/local/orchestrator && ./orchestrator --discovery=false server

The above is useful for development and testing purposes. You probably wish to keep to the defaults.

`continuous` 启动命令已删除。`server --discovery=false` 仍初始化 Raft，支持 bootstrap、成员管理、手动发现及业务操作；未 bootstrap 或失去多数派时不接受业务写入。本地维护使用 `orchestrator admin`，不会启动 Raft。
