SHELL := /bin/bash

.DEFAULT_GOAL := help

GO ?= go
GIT ?= git
DOCKER ?= docker

BINARY ?= bin/orchestrator
TOOLS_BIN ?= bin/tools
RELEASE_VERSION_FILE := $(shell sed -n '1p' RELEASE_VERSION)
RELEASE_VERSION ?=
RELEASE_SUBVERSION ?=
EFFECTIVE_RELEASE_VERSION := $(if $(strip $(RELEASE_VERSION)),$(RELEASE_VERSION),$(RELEASE_VERSION_FILE))
VERSION ?= $(EFFECTIVE_RELEASE_VERSION)$(if $(strip $(RELEASE_SUBVERSION)),_$(RELEASE_SUBVERSION))
GIT_COMMIT ?= $(shell $(GIT) rev-parse HEAD)
RACE ?= 0
RACE_FLAG := $(if $(filter 1 true yes YES,$(RACE)),-race,)
GO_ENV :=
ifneq ($(strip $(GOOS)),)
GO_ENV += GOOS=$(GOOS)
endif
ifneq ($(strip $(GOARCH)),)
GO_ENV += GOARCH=$(GOARCH)
endif

INTEGRATION_ARGS ?=
SYSTEM_TEST_ARGS ?=
GOVULNCHECK_VERSION ?= v1.7.0
GOVULNCHECK := $(TOOLS_BIN)/govulncheck-$(GOVULNCHECK_VERSION)

DOCKER_TTY ?= -it
DOCKER_EXTRA_ARGS ?=
TARBALL_URL ?=
RUN_TESTS ?= YES
ALLOW_TESTS_FAILURES ?=
MOUNT_TEST_DIR ?=
CI_ENV_REPO ?= https://github.com/percona/orchestrator-ci-env.git
CI_ENV_BRANCH ?= master
PACKAGES_PATH ?= /tmp/orchestrator-release
RUNTIME_IMAGE ?= orchestrator-alpine
TEST_IMAGE ?= orchestrator-test
CVE_IMAGE ?= orchestrator-cve
PACKAGING_IMAGE ?= orchestrator-packaging
SYSTEM_IMAGE ?= orchestrator-system
RAFT_IMAGE ?= orchestrator-raft
HUB_IMAGE ?= openarkcode/orchestrator
HUB_TAG ?= $(shell $(GIT) describe --tags --abbrev=0 2>/dev/null)

DOCKER_TEST_MOUNT :=
ifeq ($(MOUNT_TEST_DIR),YES)
DOCKER_TEST_MOUNT := --mount type=bind,source=$(CURDIR)/tests,destination=/orchestrator/tests
endif

.PHONY: help check-go check-build-tools check-docker deps fmt-check binary build test-build test-unit test-integration test-docs test-system test test-container install-govulncheck cve image run hub-image docker-test docker-test-ci docker-cve docker-cve-ci package system system-ci raft

help: ## 显示可用的构建、测试和容器入口
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check-go:
	@command -v "$(GO)" >/dev/null 2>&1 || { echo "go binary not found in PATH" >&2; exit 1; }

check-build-tools: check-go
	@command -v "$(GIT)" >/dev/null 2>&1 || { echo "git binary not found in PATH" >&2; exit 1; }
	@command -v rsync >/dev/null 2>&1 || { echo "rsync binary not found in PATH" >&2; exit 1; }

check-docker:
	@command -v "$(DOCKER)" >/dev/null 2>&1 || { echo "docker binary not found in PATH" >&2; exit 1; }

deps: check-go ## 下载并校验根模块与 go/golib 模块依赖
	@test ! -d vendor || { echo "vendor directory must not be committed; use go.mod and go.sum" >&2; exit 1; }
	$(GO) mod download
	$(GO) mod verify
	$(GO) mod tidy -diff
	$(GO) -C go/golib mod download
	$(GO) -C go/golib mod verify
	$(GO) -C go/golib mod tidy -diff

fmt-check: check-go ## 只读检查 Go 源码格式，不修改工作区
	@unformatted="$$(gofmt -s -l go)"; \
	if [[ -n "$$unformatted" ]]; then \
		echo "The following files need gofmt -s:" >&2; \
		echo "$$unformatted" >&2; \
		exit 1; \
	fi

binary: check-build-tools ## 仅构建 orchestrator 二进制
	@mkdir -p "$(dir $(BINARY))"
	$(GO_ENV) $(GO) build $(RACE_FLAG) -mod=readonly \
		-ldflags "-X main.AppVersion=$(VERSION) -X main.GitCommit=$(GIT_COMMIT)" \
		-o "$(BINARY)" ./go/cmd/orchestrator/main.go

build: binary ## 构建二进制并同步运行时资源到 bin/
	rsync -qa --delete ./resources/ "$(dir $(BINARY))resources/"

test-build: build ## 验证构建入口

test-unit: check-go ## 运行根模块与 go/golib 单元测试
	$(GO) test -mod=readonly ./go/...
	$(GO) -C go/golib test -mod=readonly ./...

test-integration: check-go ## 运行核心集成测试；可通过 INTEGRATION_ARGS 过滤
	./tests/integration/test.sh $(INTEGRATION_ARGS)

test-docs: ## 检查文档目录与本地链接
	./script/test-docs

test-system: ## 运行核心系统测试；需要已准备好的 system 环境
	./tests/system/test.sh $(SYSTEM_TEST_ARGS)

test: ## 按既有顺序串行运行本地完整测试集合
	$(MAKE) deps
	$(MAKE) fmt-check
	$(MAKE) build
	$(MAKE) test-unit
	$(MAKE) test-integration INTEGRATION_ARGS="$(INTEGRATION_ARGS)"
	$(MAKE) test-docs

test-container: ## 在 Docker 测试镜像内准备 MySQL 并运行测试集合
	./script/download-mysql
	$(MAKE) deps
	$(MAKE) fmt-check
	$(MAKE) build
	$(MAKE) test-unit
	./script/test-integration
	$(MAKE) test-docs

$(GOVULNCHECK): | check-go
	@mkdir -p "$(TOOLS_BIN)"
	GOBIN="$(abspath $(TOOLS_BIN))" $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	mv "$(TOOLS_BIN)/govulncheck" "$@"

install-govulncheck: $(GOVULNCHECK) ## 安装固定版本的 govulncheck 到 bin/tools

cve: $(GOVULNCHECK) ## 使用固定版本 govulncheck 检查已知漏洞
	GOVULNCHECK="$(abspath $(GOVULNCHECK))" ./script/test-cve

image: check-docker ## 构建最小运行时镜像
	$(DOCKER) build . -f docker/Dockerfile -t "$(RUNTIME_IMAGE)"

run: image ## 构建并启动最小运行时镜像
	$(DOCKER) run --rm $(DOCKER_TTY) -p 3000:3000 $(DOCKER_EXTRA_ARGS) "$(RUNTIME_IMAGE):latest"

hub-image: check-docker ## 按最近 Git tag 构建 Docker Hub 镜像，不推送
	@test -n "$(HUB_TAG)" || { echo "Cannot find latest tag" >&2; exit 1; }
	$(DOCKER) build . -f docker/Dockerfile -t "$(HUB_IMAGE):$(HUB_TAG)"

docker-test: check-docker ## 构建并运行容器化测试
	$(DOCKER) build . -f docker/Dockerfile.test -t "$(TEST_IMAGE)"
	$(DOCKER) run --rm $(DOCKER_TTY) \
		--env TARBALL_URL="$(TARBALL_URL)" \
		--env RUN_TESTS="$(RUN_TESTS)" \
		--env ALLOW_TESTS_FAILURES="$(ALLOW_TESTS_FAILURES)" \
		$(DOCKER_TEST_MOUNT) $(DOCKER_EXTRA_ARGS) "$(TEST_IMAGE):latest"

docker-test-ci: ## 无 TTY 运行容器化测试
	$(MAKE) docker-test DOCKER_TTY=

docker-cve: check-docker ## 构建并运行容器化漏洞检查
	$(DOCKER) build . -f docker/Dockerfile.cve -t "$(CVE_IMAGE)"
	$(DOCKER) run --rm $(DOCKER_TTY) $(DOCKER_TEST_MOUNT) $(DOCKER_EXTRA_ARGS) "$(CVE_IMAGE):latest"

docker-cve-ci: ## 无 TTY 运行容器化漏洞检查
	$(MAKE) docker-cve DOCKER_TTY=

package: check-docker ## 在打包镜像内生成 RPM、DEB 与 TGZ 到 PACKAGES_PATH
	@mkdir -p "$(PACKAGES_PATH)"
	$(DOCKER) build . \
		--build-arg RELEASE_VERSION="$(EFFECTIVE_RELEASE_VERSION)" \
		--build-arg RELEASE_SUBVERSION="$(RELEASE_SUBVERSION)" \
		-f docker/Dockerfile.packaging -t "$(PACKAGING_IMAGE)"
	$(DOCKER) run --rm $(DOCKER_TTY) -v "$(PACKAGES_PATH):/tmp/pkg" \
		"$(PACKAGING_IMAGE):latest" \
		bash -c 'find /tmp/orchestrator-release -maxdepth 1 -type f -exec cp -t /tmp/pkg {} +'

system: check-docker ## 构建并运行完整 system 测试环境
	$(DOCKER) build . -f docker/Dockerfile.system -t "$(SYSTEM_IMAGE)" \
		--build-arg ci_env_repo="$(CI_ENV_REPO)" \
		--build-arg ci_env_branch="$(CI_ENV_BRANCH)"
	$(DOCKER) run --rm $(DOCKER_TTY) -p 3000:3000 \
		--env TARBALL_URL="$(TARBALL_URL)" \
		--env RUN_TESTS="$(RUN_TESTS)" \
		--env ALLOW_TESTS_FAILURES="$(ALLOW_TESTS_FAILURES)" \
		$(DOCKER_TEST_MOUNT) $(DOCKER_EXTRA_ARGS) "$(SYSTEM_IMAGE):latest"

system-ci: ## 无 TTY 运行完整 system 测试环境
	$(MAKE) system DOCKER_TTY=

raft: check-docker ## 构建并运行三节点 Raft 演示环境
	$(DOCKER) build . -f docker/Dockerfile.raft -t "$(RAFT_IMAGE)"
	$(DOCKER) run --rm $(DOCKER_TTY) -p 3007:3007 -p 3008:3008 -p 3009:3009 \
		$(DOCKER_EXTRA_ARGS) "$(RAFT_IMAGE):latest"
