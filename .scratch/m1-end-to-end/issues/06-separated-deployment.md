# 06: 分离部署

**What to build:** Gate 与 Node 分别运行，经 [ADR 0006](../../../docs/adr/0006-gate-node-internal-contract.md) 的两个内部 Proto 服务走 Kratos gRPC 通信，行为与组合部署一致。链路追踪从 Gate 开始，覆盖到 Node 的转交。规格见 [M1 规格](../spec.md)。

**Blocked by:** 04 组合部署下按会话主动推送

**Status:** resolved

- [x] 内部有两个服务：Gate → Node 转交，Node → Gate 主动推送，都不对客户端暴露。
- [x] Gate 经 Kratos 服务发现选定 Node 后再转交登录；登录不要求会话 ID，成功结果通过独立的内部认证字段返回绑定用户，会话此后的请求和通知都发往这个 Node。
- [x] 转交携带可信的 Gate 实例标识、连接标识、逻辑服务和 Codec，认证后再附加会话 ID 与用户身份；Node 使用目标会话描述直接调用持有连接的 Gate 做主动推送。
- [x] 执行期限及连接关闭、服务停止引发的取消经真实 gRPC 传递到 Node 的 Handler，迟到应答和认证结果不再生效；过载拒绝遵守任务 03 的策略。
- [x] Node 下线时，绑定到它的会话被断开。
- [x] 03 和 04 的全部场景在分离部署下通过；测试在测试进程内起独立的 Gate 与 Node，经本机真实 gRPC 通信。
- [x] 同一份业务实现可以切换组合部署和分离部署，不需要改代码。
- [x] 链路追踪复用 Kratos 的 tracing 中间件，从 Gate 开始，覆盖到 Node 的转交；指标不以用户、会话或 RequestID 为标签。

## Comments

2026-10-24：已实现内部 Forward/Deliver gRPC、Kratos 服务发现、固定 Node、下线清理和 Gate 到 Node 的追踪。全部交互与限制场景在真实本机 gRPC 下通过；TestNodeOffline、TestIdleRPCPreservesSession 和 TestTracing 通过。
