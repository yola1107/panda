# 02: TCP 入口

**What to build:** Go 参考客户端经 TCP 入口连上 Gate。心跳、空闲超时和超长包的行为与 WebSocket 入口一致，上层复用同一种连接抽象。规格见 [M1 规格](../spec.md)。

**Blocked by:** 01 WebSocket 入口与心跳

**Status:** ready-for-agent

- [ ] 入口按配置绑定一个 TCP 监听端口。
- [ ] TCP 分帧采用大端序 uint32 长度前缀。
- [ ] 01 中的心跳、空闲超时和超长包场景在 TCP 上同样通过。
- [ ] TCP 分帧解码有 fuzz 测试：任意输入都不 panic，内存分配不超过最大包长。
- [ ] Go 参考客户端可以选择 TCP 或 WebSocket 连接。
