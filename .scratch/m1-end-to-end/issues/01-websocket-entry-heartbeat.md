# 01: WebSocket 入口与心跳

**What to build:** Go 参考客户端经 WebSocket 入口连上 Gate，定时发送心跳并收到 Gate 的心跳回复。空闲超时内没有任何消息时，Gate 断开连接；收到超过最大包长的消息时，Gate 也断开连接。
本任务建立客户端协议模块（[ADR 0004](../../../docs/adr/0004-client-protocol-envelope.md) 的 13 字节固定头部和 WebSocket 分帧），以及接缝 A 的端到端测试工具，后续任务复用这套工具。规格见 [M1 规格](../spec.md)。

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] 入口按配置绑定一个 WebSocket 路径，多个路径可以共用一个端口。
- [x] 客户端发出心跳后，收到 Gate 回复的心跳，且心跳不会转交给 Node。
- [x] 空闲超时到达后，连接被断开。
- [x] 消息超过入口配置的最大包长（默认 64 KiB）时，连接被断开。
- [x] 信封编码后再解码，能还原原来的消息；头部各字段采用大端序。
- [x] WebSocket 分帧解码有 fuzz 测试：任意输入都不 panic，内存分配不超过最大包长。
- [x] 端到端测试经真实 WebSocket 连接运行，测试之间相互独立，各自监听随机端口。
- [x] 连接关闭后，相关资源全部释放，相关 goroutine 退出。

## Comments

2026-10-24：已实现 WebSocket 路径、心跳、认证与空闲期限、有界解码和连接退出。TestInteractions、TestKeepAlive、TestIdleTimeout、TestOversizedFrame、TestWebSocketTextRejected 及 FuzzWebSocket 通过；端到端场景使用随机端口。
