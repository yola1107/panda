# panda

Kratos 多服务仓库，共用根目录的 `github.com/yola1107/panda` module。
当前包含最小 HelloWorld API 和开发配置，尚未添加服务实现。

## 开发命令

开发环境使用 Go 1.27.3 或更高版本。

在仓库根目录执行：

```sh
make init       # 安装 Wire、Buf 和 golangci-lint
make api        # 生成 Protobuf、gRPC、HTTP 代码和 openapi.yaml
make generate   # 执行 go generate 并整理依赖
make all        # 依次执行 api、generate
make build      # 编译全部包；有服务入口后，将可执行文件输出到 bin/
make test       # 运行全部包的单元测试
make help       # 查看所有目标
```

`api/helloworld/v1/hello.proto` 只定义 `GreeterService.SayHello`，HTTP 路径为 `GET /v1/hello/{name}`。
生成方式沿用 `kratos-layout` 的 Buf 配置，插件版本固定在 `buf.gen.yaml`，通过 `go run` 调用。
Go 依赖版本参考该模板；工具安装可由 Go 自动下载所需工具链。

## Lint

以 `golang-lint` 技能的标准模板为基线，启用其中 47 个 linter，保留其阈值及 `gofumpt` / `goimports` 格式配置。项目适配包括：

- 显式启用规则，避免工具升级时默认集合变化。
- 使用 `gci` 将导入按标准库、项目包、第三方包排序，组间留空行；项目包根据根目录 module 自动识别。
- 移除 `nolintlint` 及其抑制注释格式要求；[AGENTS.md](AGENTS.md) 规定手写 Go 代码禁止使用 `//nolint`。
- 将已弃用的 `gofumpt.extra-rules` 改为等价的 `extra.group-params` 与 `extra.clothe-returns`，写法见 [格式工具文档](https://golangci-lint.run/docs/formatters/configuration/#gofumpt)。
- 按标准生成标记排除 Protobuf、Wire、Ent 产物；手写代码、测试、schema、examples 和 testutils 仍在检查范围内。
- 仅按目录排除 `vendor` 和 `third_party`，检查时以只读模式处理根 module 依赖。

配置语法及生成文件识别方式见 [golangci-lint 官方文档](https://golangci-lint.run/docs/configuration/file/)。

本配置使用 `golangci-lint v2.15.0` 验证，由 `make init` 安装。

确保 Go 的安装目录（`GOBIN`，未设置时为 `$(go env GOPATH)/bin`）在 `PATH` 中。

单独校验配置：

```sh
make lint-config
```

从根目录运行代码检查：

```sh
make fmt        # 格式化
make lint-fix   # 按需自动修复，之后审阅 diff
make lint       # 检查整个 module 中的服务、共享包和测试
go test ./...
```

golangci-lint 没有全面禁用 `//nolint` 的配置开关，移除 `nolintlint` 也不会使抑制注释失效。这里通过开发约定禁止使用，参见 [上游说明](https://github.com/golangci/golangci-lint/discussions/3015)。

`make lint` 也会报告已启用格式工具发现的问题，不修改源文件；命令行为见 [官方 CLI 文档](https://golangci-lint.run/docs/configuration/cli/)。

AI 开发约束见 [AGENTS.md](AGENTS.md)。
