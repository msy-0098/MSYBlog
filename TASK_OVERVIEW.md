# 博客任务总览（后端）

> 多 Agent / 多人并行时先看本文件，再改代码。  
> 配套前端：`frontend/TASK_OVERVIEW.md`（内容应对齐「当前阶段」与阶段表）。  
> 详细设计与逐步计划在 `docs/superpowers/`，本文件只做**进度与边界**总览。

**最后更新**：2026-07-24  
**当前阶段**：**Phase 2 — 管理端 AI 增强（进行中）**  
**当前执行方（约定）**：Codex 主改 Phase 2；其它 Agent 不要抢同一批文件，除非已在本文件改写「归属」。

---

## 1. 阶段一览

| 阶段 | 主题 | 状态 | 规格 / 计划 |
| --- | --- | --- | --- |
| Phase 1 | 验证码冷却、友好错误、品牌邮件 | 基本完成 | `docs/superpowers/specs/2026-07-23-blog-experience-ai-geoip-pagination-design.md` · `plans/2026-07-23-phase-1-verification-errors-mail-backend.md` |
| **Phase 2** | **Admin AI：Provider / Runtime / 健康检查 / 会话可靠性 / 润色 / 运营报告** | **进行中** | 同上 spec §6–9 · `plans/2026-07-23-phase-2-admin-ai-backend.md` |
| Phase 3 | DB-IP Lite GeoIP 查询 | 未开始（仅文档） | `plans/2026-07-23-phase-3-geoip-backend.md` |
| Phase 4 | 全站分页（评论游标、管理端列表等） | 部分已有 / 未收口 | `plans/2026-07-23-phase-4-pagination-backend.md` |
| 旁路 | 消息通知（站内 + 评论邮件） | **已完成** | 模型/API/铃铛/通知中心/设置偏好；评论/封禁/注册写入 |

状态含义：

- **未开始**：无实现或仅有 docs  
- **进行中**：有人在改，以本文件「当前阶段」为准  
- **基本完成**：主路径已上线，允许小修  
- **已收口**：阶段 exit gate 通过并完成提交推送  

---

## 2. 当前阶段：Phase 2（AI）

### 2.1 目标（后端）

- Provider 抽象与稳定错误分类  
- Runtime 限流 / 并发 / 输入长度限制  
- 健康检查 API  
- 会话消息可靠性、重试关系、用量与（如计划要求）游标分页  
- 文章润色结果结构化  
- 运营报告模型与 CRUD（按 plan 范围）  

### 2.2 主要触碰范围（Phase 2 归属）

优先视为 **Codex / Phase 2 领地**，其它任务请避开或先更新本文件：

```text
internal/ai/**
internal/service/ai_*.go
internal/handler/admin_ai_handler*.go
internal/handler/admin_insight_handler*.go
internal/model/ai*.go
internal/router/admin_ai_routes*.go
internal/router/router.go          # 仅 AI 路由注册相关改动
cmd/server/main.go                 # 仅 AI 依赖注入相关
```

### 2.3 建议不要并行改的文件（与 AI 强耦合）

- 上述 AI 文件 + 会整文件重写 `router.go` 大段逻辑的其它任务  
- 未约定前不要在同一 PR 混入 GeoIP / 通知 / 大范围分页  

### 2.4 完成判定（摘要）

以 `plans/2026-07-23-phase-2-admin-ai-backend.md` 的 Task 与 Final Verification 为准；至少：

- 相关 `go test` 通过  
- 路由与鉴权行为符合设计  
- 提交并推送 `origin` + `gitee` 后，将本文件 Phase 2 改为「基本完成 / 已收口」，并把「当前阶段」切到 Phase 3 或下一项  

---

## 3. 后续阶段（未开工时只读文档）

### Phase 3 — GeoIP

- 新目录预期：`internal/geoip/**`、`cmd/update-dbip` 等（以 plan 为准）  
- 与 AI 文件重叠少，可在 Phase 2 **提交推送完成后**开工  

### Phase 4 — 分页

- 评论游标、管理端多列表 page/pageSize、索引  
- 可能触及 blog/comment/admin list handlers；与 AI 重叠中等，建议 Phase 2 收口后再动  

### 旁路 — 通知（未立项进仓）

- 预期：`notifications` 模型、评论 pending 写入、admin 列表/已读、可选 SMTP  
- 会碰评论创建链路与 `mail`、admin 路由；**不要与 Phase 2 同时改同一批文件**  

---

## 4. 多 Agent 协作规则

1. **开工前**：读本文件「当前阶段」与触碰范围。  
2. **换阶段 / 换执行方**：先改本文件状态与归属，再写代码。  
3. **禁止**：回退他人未提交改动；`git reset --hard` / 覆盖别人文件。  
4. **提交**：只 stage 本任务相关文件；禁止提交 `_*.py`、本地日志、`.env`、数据库、上传物。  
5. **推送**：`backend/` 内分别 `git push origin <branch>` 与 `git push gitee <branch>`。  
6. **前后端**：同一阶段应前后端成对推进；后端先接口、前端跟合同，或按 plan 写明的顺序。  

---

## 5. 文档索引

| 类型 | 路径 |
| --- | --- |
| 总规格 | `docs/superpowers/specs/2026-07-23-blog-experience-ai-geoip-pagination-design.md` |
| Phase 1 计划 | `docs/superpowers/plans/2026-07-23-phase-1-verification-errors-mail-backend.md` |
| Phase 2 计划 | `docs/superpowers/plans/2026-07-23-phase-2-admin-ai-backend.md` |
| Phase 3 计划 | `docs/superpowers/plans/2026-07-23-phase-3-geoip-backend.md` |
| Phase 4 计划 | `docs/superpowers/plans/2026-07-23-phase-4-pagination-backend.md` |
| 前端总览 | 同 monorepo 工作区下 `../frontend/TASK_OVERVIEW.md`（独立 git 仓库） |

---

## 6. 变更记录（简）

| 日期 | 说明 |
| --- | --- |
| 2026-07-24 | 建立本总览；当前阶段标为 Phase 2 AI 进行中 |
| 2026-07-24 | 落地管理端消息通知（站内 + 评论邮件偏好） |
