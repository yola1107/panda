---
status: accepted
---

# 保留 Gate 与 Node，以 Kratos 服务替代 Due Mesh

Panda 保留 Gate 与 Node 两种角色：Gate 承担客户端长连接接入，Node 处理 Gate 转交的业务消息。
独立无状态服务使用标准 Kratos 微服务，替代 Due Mesh 的服务角色。
首期实现 WS 接入，为 TCP 保留后续接入位置；当前不实现 TCP 监听或读写。

## 取舍

保留 Gate 与 Node 的职责划分，便于分别处理连接接入与长连接业务消息；独立无状态服务沿用 Kratos 的开发与治理方式。
这项决策只确认角色边界，不等于照搬 Due 的内部通信、路由或 Actor 实现。
Gate 与 Node 的运行方式、内部通信、状态归属和回推契约仍需确认。

新应用无需兼容旧 Due 客户端协议或服务 API。
