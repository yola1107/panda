---
status: accepted
---

# 保留 Gate 与 Node，以 Kratos 服务替代 Due Mesh

Panda 保留 Gate 与 Node 两种角色：Gate 承担客户端长连接接入，Node 处理 Gate 转交的业务消息。
不设 Mesh；独立无状态服务使用标准 Kratos 微服务。
Gate 与 Node 可组合在同一进程，也可分离部署：同进程走本地调用，跨进程走 Kratos RPC。
本地调用同样经过身份校验、中间件、调度和错误契约，组合部署不获得额外权限。
首期实现 TCP 和 WebSocket 接入。

## 取舍

保留 Gate 与 Node 的职责划分，便于分别处理连接接入与长连接业务消息；独立无状态服务沿用 Kratos 的开发与治理方式。
同一组件实现支持组合与分离部署，不维护两套业务实现；本地调用与 RPC 统一的是调用契约，不宣称故障行为相同。
这项决策不照搬 Due 的内部通信、路由或 Actor 实现。

新应用无需兼容旧 Due 客户端协议或服务 API。
