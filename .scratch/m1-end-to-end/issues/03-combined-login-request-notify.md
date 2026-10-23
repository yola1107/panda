# 03: 组合部署下的登录、请求、应答与通知

**What to build:** 在组合部署下，客户端先发登录请求完成认证，之后发请求并收到应答，还能发通知。Handler 先手写注册，不依赖生成器。认证规则见 [ADR 0005](../../../docs/adr/0005-login-request-authentication.md)，规格见 [M1 规格](../spec.md)。

**Blocked by:** 01 WebSocket 入口与心跳

**Status:** resolved

- [x] 认证前，Gate 只放行心跳和逻辑服务声明的登录请求；其他请求被拒绝。
- [x] 认证超时仍未登录时，连接被断开。
- [x] Gate 先选定 Node，再以连接上下文转交登录，不要求会话 ID 或可信用户身份；登录 Handler 通过独立于业务 payload 的认证结果返回绑定用户。
- [x] Gate 只接受连接仍未认证、该次登录仍有效的成功结果并原子创建会话；失败、超时或断连后的迟到结果不会创建或变更会话。
- [x] 登录后，请求能收到 RequestID 对应的应答；同一连接上的并发请求不会串号。
- [x] 认证后的 Handler 能拿到可信的用户身份和会话；登录 Handler 仅获得连接上下文。
- [x] Handler 返回 Kratos 错误时，客户端收到带同一 reason 的失败应答，并能还原为 Kratos 错误。
- [x] 通知能到达 Handler，客户端不会收到应答。
- [x] Gate 只透传 payload，由 Node 按入口的编码器解码。
- [x] 转交中的逻辑服务和 Codec 标识由 Gate 按入口配置写入；同一逻辑服务分别使用 Protobuf 和 JSON 入口时，登录、请求和错误应答均正确，Node 不支持的 Codec 被明确拒绝。
- [x] 执行超时沿本地调用传递到 Handler；慢 Handler 收到取消，客户端收到明确的超时错误，迟到成功结果不再发送。连接关闭或服务停止时取消在途调用。
- [x] 登录、业务请求和通知都受单连接在途转交上限与 Node 实例执行上限约束；满额时请求返回过载错误，通知使连接断开，不无界排队或创建等待名额的无限 goroutine。
- [x] 已取消调用不会进入尚未开始的 Handler；超时但仍未退出的 Handler 继续占用 Node 执行名额，实际退出后才释放。
- [x] Go 客户端在调用超时、取消或连接关闭后清理等待项，迟到应答不影响其他请求，不自动重试；单次调用取消不发送新增的客户端协议消息。
- [x] 组合部署走本地调用，并经过和 RPC 相同的身份校验、中间件与错误契约。
- [x] Gate 和 Node 通过显式组装创建，交给 kratos.App 运行。

## Comments

2026-10-24：已实现登录认证、可信上下文、RequestID 关联、Kratos 错误、通知、执行期限及 Gate/Node 上限。TestInteractions、TestTrustedContext、TestExecutionTimeout、TestLateLogin、TestDisconnectedLogin、TestCanceledHandlerDoesNotStart、TestConnectionLimitAndCancellation、TestNodeLimitRetainedUntilHandlerExit、TestClientCancellation、TestGateStopCancelsHandlers 和 TestNotifyHasNoResponse 通过。
