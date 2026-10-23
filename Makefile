DIR ?= .
GOLANGCI_LINT ?= golangci-lint
BUF ?= buf
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.DEFAULT_GOAL := help
.PHONY: init api generate build test all lint-config lint lint-fix fmt help

init: ## 安装开发工具
	go install github.com/google/wire/cmd/wire@v0.7.0
	go install github.com/bufbuild/buf/cmd/buf@v1.74.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0

api: ## 生成 Protobuf、gRPC、HTTP 代码和 OpenAPI 文档
	$(BUF) generate --template buf.gen.yaml

generate: ## 生成代码并整理共享 module 的依赖
	go generate ./...
	go mod tidy

build: ## 编译全部包，将服务可执行文件输出到 bin/
	go build ./...
	@main_packages=$$(go list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./...) && \
	if [ -n "$$main_packages" ]; then \
		mkdir -p bin && \
		go build -ldflags "-X main.Version=$(VERSION)" -o ./bin/ $$main_packages; \
	fi

test: ## 运行全部包的单元测试
	go test ./...

all: ## 依次生成 API 和其他代码
	$(MAKE) api
	$(MAKE) generate

lint-config: ## 校验 lint 配置
	$(GOLANGCI_LINT) config verify --config .golangci.yml

lint: ## 检查 Go 代码和测试（支持 DIR，默认全仓）
	cd "$(DIR)" && $(GOLANGCI_LINT) run --config "$(CURDIR)/.golangci.yml" ./...

lint-fix: ## 自动修复 lint 和格式问题（支持 DIR，默认全仓）
	cd "$(DIR)" && $(GOLANGCI_LINT) run --config "$(CURDIR)/.golangci.yml" --fix ./...

fmt: ## 格式化手写 Go 代码（支持 DIR，默认全仓）
	cd "$(DIR)" && $(GOLANGCI_LINT) fmt --config "$(CURDIR)/.golangci.yml" ./...

help: ## 显示帮助
	@printf '用法：make <目标> [DIR=目录]\n\n'
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
