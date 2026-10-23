# panda

基于 Kratos 的长连接框架，共用根目录的 `github.com/yola1107/panda` module。
M1 提供 TCP/WebSocket 接入、登录认证、请求与通知、按会话主动推送，以及本地和 gRPC 两种 Gate/Node 部署。原有 HelloWorld API 保留。

## 运行示例

组合部署，在一个终端运行：

```sh
go run ./cmd/panda-example -mode combined
```

在另一终端运行 Go 参考客户端。每次演示登录、请求应答、通知、业务主动推送和心跳：

```sh
go run ./cmd/panda-example -mode client -entry 0
```

`-entry 0/1/2/3` 分别选择 TCP Protobuf、WebSocket Protobuf、TCP JSON、WebSocket JSON。
WebSocket 的 `/echo/proto` 和 `/echo/json` 共用 `18080` 端口。
示例登录令牌是 `demo-token`，只用于演示业务认证入口。

分离部署时，先停止组合部署，再在两个终端分别运行：

```sh
go run ./cmd/panda-example -mode node
go run ./cmd/panda-example -mode gate
```

客户端命令保持相同。两种部署共用 [Echo Handler](examples/echo/handler.go) 和 [proto 契约](api/echo/v1/echo.proto)。
配置由 Kratos Config 加载，可用 `-config` 指定路径，默认 [examples/echo/config.yaml](examples/echo/config.yaml)。
示例使用静态 Kratos 服务发现表；实际应用向 `remote.NewNodes` 和 `remote.NewPusher` 注入自己的 `registry.Discovery`。
Node 按逻辑服务全限定名注册，Gate 使用 `panda.gate` 服务名，实例 ID 与 `gate.Config.ID` 一致。
登录选定 Node 后固定调用该实例；发现移除或 RPC 健康检查失败会断开绑定会话；正常 RPC 空闲会先唤醒连接并确认健康。

内部 gRPC 的 `Node.Forward` 和 `Gate.Deliver` 只接受可信 Gate/Node 调用，不能暴露到客户端网络。
示例监听本机地址；实际部署使用受控内部网络，并通过 Kratos gRPC 的 TLS 或认证中间件配置调用方身份。
本地与 RPC 调用共用 Node 的连接/会话校验、业务中间件、执行名额和错误转换。

## 声明业务

在 proto 方法上声明 `(panda.route.v1.route)`，指定 `number` 和 `kind`：`KIND_LOGIN`、`KIND_REQUEST` 或 `KIND_NOTIFY`。
每个逻辑服务有一个登录方法。主动推送消息声明 `(panda.route.v1.push)`，同时指定 `number` 和所属 service 全限定名。
同一服务中的请求、通知和推送共用编号表；不同服务可复用编号。

`make api` 调用本仓库的 Buf 插件 `protoc-gen-go-panda`，生成 `*_panda.pb.go`：

- `<Service>Handler`：登录返回业务应答、可信用户 ID 和错误；通知只返回错误。
- `Register<Service>`：将声明的路由注册到 Node，默认支持 `proto` 和 `json`，也可显式传入支持的 Codec。
- `<Service>PandaClient`：提供类型化请求和通知，与普通 gRPC 客户端名称区分。
- `On<Message>` 和 `Push<Service><Message>`：提供类型化推送回调与按会话发送函数。

业务组装参考 [示例入口](cmd/panda-example/main.go)。先构造 Node、注册 Handler，再显式连接 Gate 或 RPC 适配器，并交给 `kratos.App` 管理。
`session.ConnectionFromContext` 在登录时提供入口决定的连接信息；`session.FromContext` 只在认证后的 Handler 中提供可信会话与用户身份。
Gate 不解析业务 payload；Node 按入口 Codec 解码，推送按目标会话 Codec 编码。`json` 使用 Protobuf JSON 格式。

## 协议与限制

[协议 ADR](docs/adr/0004-client-protocol-envelope.md) 定义 13 字节大端序信封。类型编号为请求 1、应答 2、主动推送 3、通知 4、心跳 5。
TCP 添加 uint32 长度前缀，WebSocket 每个二进制消息承载一个信封。
心跳由 Gate 直接回复；参考客户端的 `KeepAlive` 可定时发送，调用方负责取消和等待。
心跳没有关联编号，因此已发出心跳超时或取消后会关闭连接，防止迟到回复被下一次心跳误用。

| 配置 | 默认值 |
| --- | --- |
| 最大包长 | 65536 字节，包含信封头，不含 TCP 长度前缀 |
| 认证超时 | 10 秒，心跳不会延长 |
| 空闲超时 | 60 秒，收到消息后重新计时 |
| 执行超时 | 5 秒，也作为单次网络写入期限 |
| 每连接发送队列 | 64 条 |
| 每连接在途转交 | 16 次 |
| 每 Node 执行上限 | 256 个 Handler |

Go 配置中的零值采用上述默认值，负值无效。示例 YAML 的时间值单位为纳秒。M1 不做热更新。
超长包和发送队列满会断开连接；队列满不会静默丢弃消息。
过载请求返回 `OVERLOADED`，执行超时返回 `TIMEOUT`；通知不产生业务应答，接收失败时断开连接。
Handler 返回的 Kratos 错误保留 code、reason、message 和 metadata。

执行期限和连接关闭会取消 Handler Context。Handler 必须响应取消；不配合的 Handler 在实际退出前仍占用 Node 名额，框架不能撤销业务副作用。
客户端调用超时或取消只结束本地等待，迟到应答被忽略，不重试请求。RequestID 在连接内递增，耗尽后须重连。
推送回调顺序执行，应响应 Context 取消，不能在回调内调用 `Close`。客户端推送队列有界，积压满时关闭连接。

追踪使用 Kratos tracing 中间件，从 Gate 开始并覆盖本地或 RPC 转交。应用设置 OpenTelemetry TracerProvider 后即可采集；框架不引入用户或会话维度的指标。
M1 不包含会话恢复、按用户推送、在线索引、Keyed Executor 或故障接管。

## 验收

[端到端测试](examples/echo/e2e_test.go) 使用真实 TCP/WebSocket 和本机 gRPC，覆盖两种部署与两种 Codec。
生成器测试动态生成、编译并运行独立临时模块；仓库自身仍只有根目录一个 Go module。

```sh
make api
make fmt
make lint-config
make lint
go test ./...
go test -race ./client ./gate ./node ./remote ./examples/echo
go test ./protocol -run '^$' -fuzz FuzzTCP -fuzztime 5s
go test ./protocol -run '^$' -fuzz FuzzWebSocket -fuzztime 5s
```

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

`fmt`、`lint` 和 `lint-fix` 支持 `DIR`，默认 `.`，递归处理整个 module。
指定目录时包含其子目录，仍使用根目录的 `.golangci.yml`：

```sh
make lint DIR=api/helloworld
make lint-fix DIR=api/helloworld
make fmt DIR=api/helloworld
```

golangci-lint 没有全面禁用 `//nolint` 的配置开关，移除 `nolintlint` 也不会使抑制注释失效。这里通过开发约定禁止使用，参见 [上游说明](https://github.com/golangci/golangci-lint/discussions/3015)。

`make lint` 也会报告已启用格式工具发现的问题，不修改源文件；命令行为见 [官方 CLI 文档](https://golangci-lint.run/docs/configuration/cli/)。

AI 开发约束见 [AGENTS.md](AGENTS.md)。
