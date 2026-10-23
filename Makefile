GOLANGCI_LINT ?= golangci-lint
BUF ?= buf
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: init
# install development tools
init:
	go install github.com/google/wire/cmd/wire@v0.7.0
	go install github.com/bufbuild/buf/cmd/buf@v1.74.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0

.PHONY: api
# generate Protobuf, gRPC, HTTP code and OpenAPI docs
api:
	$(BUF) generate --template buf.gen.yaml

.PHONY: generate
# generate code and tidy shared module dependencies
generate:
	go generate ./...
	go mod tidy

.PHONY: build
# build all packages and service binaries into bin
build:
	go build ./...
	@main_packages=$$(go list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./...) && \
	if [ -n "$$main_packages" ]; then \
		mkdir -p bin && \
		go build -ldflags "-X main.Version=$(VERSION)" -o ./bin/ $$main_packages; \
	fi

.PHONY: test
# run unit tests for all packages
test:
	go test ./...

.PHONY: all
# generate API and other code
all:
	$(MAKE) api
	$(MAKE) generate

.PHONY: lint-config
# verify lint configuration
lint-config:
	$(GOLANGCI_LINT) config verify --config .golangci.yml

.PHONY: lint
# lint all packages and tests in the shared module
lint:
	$(GOLANGCI_LINT) run --config .golangci.yml ./...

.PHONY: lint-fix
# apply lint fixes and formatting
lint-fix:
	$(GOLANGCI_LINT) run --config .golangci.yml --fix ./...

.PHONY: fmt
# format handwritten Go source
fmt:
	$(GOLANGCI_LINT) fmt --config .golangci.yml ./...

.PHONY: help
# show help
help:
	@echo ''
	@echo 'Usage:'
	@echo ' make [target]'
	@echo ''
	@echo 'Targets:'
	@awk '/^[a-zA-Z_0-9-]+:/ { \
		helpMessage = match(lastLine, /^# (.*)/); \
		if (helpMessage) { \
			helpCommand = substr($$1, 1, index($$1, ":") - 1); \
			helpMessage = substr(lastLine, RSTART + 2, RLENGTH); \
			printf "\033[36m%-22s\033[0m %s\n", helpCommand, helpMessage; \
		} \
	} \
	{ lastLine = $$0 }' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help
