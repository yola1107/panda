# 02: TCP 入口

**What to build:** Go 参考客户端经 TCP 入口连上 Gate。心跳、空闲超时和超长包的行为与 WebSocket 入口一致，上层复用同一种连接抽象。规格见 [M1 规格](../spec.md)。

**Blocked by:** 01 WebSocket 入口与心跳

**Status:** resolved

- [x] 入口按配置绑定一个 TCP 监听端口。
- [x] TCP 分帧采用大端序 uint32 长度前缀。
- [x] 01 中的心跳、空闲超时和超长包场景在 TCP 上同样通过。
- [x] TCP 分帧解码有 fuzz 测试：任意输入都不 panic，内存分配不超过最大包长。
- [x] Go 参考客户端可以选择 TCP 或 WebSocket 连接。

## Comments

2026-10-24：已实现 TCP 大端长度前缀，复用连接生命周期和客户端接口。TestTCPFrame、FuzzTCP 及 TCP 端到端矩阵通过。
