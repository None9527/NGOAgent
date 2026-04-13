# R5 生产化执行图

> Updated: 2026-04-13
> Status: R5 in progress

## 目标

R5 的目标是把 R4 之后边界清晰的 runtime 封装成可生产运行、可观测、可部署、可隔离、可运维的服务形态。

R5 不重写 runtime 内核，不重新定义 plugin 边界，也不在收尾阶段大改 API shape。它只围绕生产运行所需的外层保障做闭环。

## 完整 Plan

### P1：Production Readiness

目标：系统能回答“当前是否可服务、哪些关键依赖缺失、启动多久”。

已完成：

- `/v1/health` 保留原 `status/version/model/tools` 字段。
- `/v1/ready` 提供匿名 readiness probe，使用 `200/503` 表达可服务状态。
- `HealthResponse` 增加 `ready`、`started_at`、`uptime_seconds`、`checks`。
- `HealthResponse` 额外暴露 capability category / source summary，便于 operator 判断当前 runtime 暴露面。
- `AdminQueries.Health` 对 config / router / tools / runtime / discovery / security / sessions / persistence 做状态汇总。
- 关键依赖缺失时返回 `degraded`，避免 nil dependency panic。

完成标准：

- health 响应能支持 load balancer / operator 判断 readiness。
- readiness probe 不要求探针解析完整 payload 即可判断是否可服务。
- operator 能从 health 直接看见 builtin / MCP / skill 等 capability 暴露概况。
- 旧字段兼容，新增字段为生产化补充。

### P2：Observability

目标：启动期能看见能力来源和动态 source。

已完成：

- builtin tool provider 输出 provider manifest 摘要。
- runtime capability source 输出 source 启动摘要。
- discovery capability 保留 builtin provider / MCP server / skill path source。

完成标准：

- operator 能从日志和 discovery 结果定位能力来自哪里。
- 默认工具集合和启动顺序不因观测增强改变。

### P3：Transport Safety

目标：HTTP/gRPC 继续通过 capability contract 运行，避免生产化改动穿透 application internals。

已完成：

- transport contract 在 R4 closure 中复查完毕。
- HTTP/gRPC 构造路径使用 `ApplicationServices.HTTPTransport()` / `GRPCTransport()`。
- `/v1/health` 与 `/v1/ready` 继续匿名可访问，其余 API 仍走已有 auth middleware。

完成标准：

- R5 不引入新的 legacy facade 依赖。
- API shape 只做向后兼容扩展。

### P4：Rate Limit / Tenant / Deployment Boundaries

目标：明确后续生产化边界，避免在当前 closure 中做无合同的大改。

本轮不落代码：

- rate limiting 需要先固定请求身份、session scope 和工具调用 scope。
- tenant isolation 需要先固定 workspace/session/data ownership。
- deployment packaging 需要结合目标平台决定 systemd/docker/k8s/render 等输出。

完成标准：

- 当前代码保留插入点，不在未定义 identity/tenant 合同前做半成品限流或隔离。

## 收尾验收

- `go test ./...` 通过。
- `git diff --check` 通过。
- health/readiness 有单元测试覆盖 ready/degraded 路径。
- R4 的 provider/source/discovery 观测面可被 R5 health 与生产运维语义复用。
- GitNexus 对核心启动和 discovery 路径会报 HIGH/CRITICAL；本轮风险按 diff 复查和测试覆盖处理，不改变默认启动顺序、工具集合或 transport contract。
