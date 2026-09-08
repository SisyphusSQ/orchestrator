# Requires Docker 17.09 or later (multi stage builds)
#
# Orchestrator will look for a configuration file at /etc/orchestrator.conf.json.
# It will listen on port 3000.
# If not present a minimal configuration will be generated using the following environment variables:
#
# Default variables which can be used are:
#
# ORC_TOPOLOGY_USER (default: orchestrator): username used by orchestrator to login to MySQL when polling/discovering
# ORC_TOPOLOGY_PASSWORD (default: orchestrator):  password needed to login to MySQL when polling/discovering
# ORC_DB_HOST (default: orchestrator):  orchestrator backend MySQL host
# ORC_DB_PORT (default: 3306):  port used by orchestrator backend MySQL server
# ORC_DB_NAME (default: orchestrator): database named used by orchestrator backend MySQL server
# ORC_USER (default: orc_server_user): username used to login to orchestrator backend MySQL server
# ORC_PASSWORD (default: orc_server_password): password used to login to orchestrator backend MySQL server

FROM node:22.22.2-bookworm-slim AS web-build
WORKDIR /web
RUN npm install --global pnpm@10.33.2
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.26.8-alpine3.23 as build

RUN apk --no-cache add libcurl rsync gcc g++ build-base bash git make

WORKDIR /orchestrator
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /web/dist/ /orchestrator/web/assets/
ENV WEB_PREBUILT=1
RUN make build

FROM alpine:3.23

RUN apk --no-cache add bash curl jq

EXPOSE 3000

COPY --from=build /orchestrator/bin/ /usr/local/orchestrator/
COPY --from=build /orchestrator/bin/orch /usr/bin/orch
COPY --from=build /orchestrator/conf/orchestrator-sample*.conf.json /usr/local/orchestrator/
COPY --from=build /orchestrator/conf/orchestrator-sample-sqlite.conf.json /etc/orchestrator.conf.json

WORKDIR /usr/local/orchestrator
COPY docker/entrypoint.sh /entrypoint.sh
CMD ["/entrypoint.sh"]
