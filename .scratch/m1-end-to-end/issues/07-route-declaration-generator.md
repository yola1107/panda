# 07: 路由声明与生成器

**What to build:** 业务开发者以 proto service 标识一个逻辑服务，声明其请求、通知、登录请求和主动推送，并为它们指定数字路由。运行 `make api` 后，生成 Handler 接口、注册代码、Go 客户端调用和类型化推送分发代码，复用 03、04 的运行时能力，替代手写的注册与推送解码。规格见 [M1 规格](../spec.md)。

**Blocked by:** 04 组合部署下按会话主动推送

**Status:** resolved

- [x] 自定义方法选项声明请求、通知和登录请求的编号与类型，归属为所在 service；主动推送消息的选项同时声明编号和所属 service 的全限定名，遵守 [ADR 0003](../../../docs/adr/0003-numeric-route-per-logical-service.md)。
- [x] 生成器作为 Buf 插件接入现有的 `make api`。
- [x] 生成的代码能编译，生成的注册代码与 proto 声明一致。
- [x] 同一逻辑服务内编号重复时，生成失败，并报出冲突的编号。
- [x] 不同逻辑服务可以使用相同编号。
- [x] 同一 proto 文件包含两个逻辑服务、各有编号为 1 的主动推送时，生成成功并按各自所属服务分发到正确的消息类型；归属缺失或引用不存在的 service 时生成失败。
- [x] 请求、通知与主动推送在同一逻辑服务内共用编号表，跨消息类型的重复编号也会被拒绝。
- [x] 生成的 Go 客户端调用发出请求，并返回类型化应答，调用方不需要手写编号。
- [x] 生成的推送分发代码按客户端入口的 Codec 解码 payload，再调用类型化回调；业务调用方不手写路由号或解码，Protobuf 与 JSON 均通过验收。
- [x] 生成器测试覆盖以上行为（接缝 B）。
- [x] 生成文件带标准的生成标记，并重新生成提交。

## Comments

2026-10-24：已接入 make api，生成 Handler、注册代码、类型化客户端及推送回调。TestGeneratedCode 动态生成、编译并运行两个服务；TestDuplicateRoute、TestPushOwnership 和 TestGeneratedNameCollision 验证冲突与归属错误。生成产物已更新。
