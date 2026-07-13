# 管理端 AI、PostgreSQL 与流式服务重构设计

日期：2026-07-13  
状态：用户已批准  
适用仓库：backend

## 1. 背景

当前管理工作台把普通统计、同步 AI 洞察和单轮 AI 问答放在同一页面。DeepSeek 调用会阻塞 dashboard 响应，前端又对整个工作台使用 loading 遮罩。聊天接口使用非流式 JSON 响应，没有会话持久化、历史管理或取消生成能力。

项目的生产配置、部署文档和 GitHub Actions 已具备 PostgreSQL 基础，但仍需将现有 SQLite 历史数据安全迁移到服务器已有 PostgreSQL 实例，并新增 AI 会话数据结构。

## 2. 已批准目标

- 采用分层渐进式改造，不重写现有博客业务。
- 全站正式数据源统一为服务器现有 PostgreSQL。
- 使用短暂停写维护窗口完成 SQLite 全量迁移。
- AI 助手使用独立工作区和服务端 PostgreSQL 会话历史。
- AI 回复通过 SSE 真正流式输出，并支持停止生成。
- dashboard 普通统计先返回，AI 洞察独立异步加载。
- 复用现有 GitHub CI/CD；普通 push 不自动执行生产历史数据迁移。

## 3. 服务边界

### 3.1 Dashboard

`GET /api/admin/dashboard` 只查询 PostgreSQL 中的普通统计、访问数据和近期内容，不调用 DeepSeek。

AI 洞察拆为独立接口：

```text
POST /api/admin/ai/insights/generate
```

该接口失败时不影响 dashboard 其他数据。前端在卡片范围内展示加载、刷新、错误和重试状态。

### 3.2 AI 会话服务

会话历史以 PostgreSQL 为唯一事实来源。浏览器只提交当前用户输入，后端负责：

1. 校验管理员 JWT；
2. 校验会话存在且可访问；
3. 写入用户消息；
4. 从数据库读取最近上下文；
5. 注入服务端系统提示词；
6. 调用 DeepSeek 流式接口；
7. 增量输出 SSE；
8. 保存 AI 完整或中止后的部分消息；
9. 更新会话统计与标题。

前端不得上传可任意篡改的完整历史作为正式上下文。

## 4. PostgreSQL 数据模型

### 4.1 `ai_conversations`

- `id`：主键；
- `title`：会话标题；
- `title_mode`：自动或手动，用于防止手动标题被覆盖；
- `created_by`：管理员 ID；
- `model`：默认模型标识；
- `message_count`：消息数量；
- `last_message_at`：最近消息时间；
- `created_at`、`updated_at`：审计时间。

第一条用户消息发送后，以截断后的用户文本生成标题，不额外调用 AI。手动重命名后不再自动覆盖。

### 4.2 `ai_messages`

- `id`：主键；
- `conversation_id`：所属会话；
- `role`：`user`、`assistant` 或 `system`；
- `content`：消息正文；
- `status`：`streaming`、`completed`、`aborted` 或 `failed`；
- `sequence`：会话内顺序；
- `model`：实际模型；
- `error_message`：失败信息；
- `created_at`、`updated_at`：审计时间。

约束：

- `(conversation_id, sequence)` 唯一；
- 会话删除时关联消息在事务中级联删除；
- 同一会话同一时刻只允许一个生成任务；
- 会话列表按 `last_message_at` 倒序。

## 5. 管理 API

```text
GET    /api/admin/ai/conversations
POST   /api/admin/ai/conversations
GET    /api/admin/ai/conversations/:id
PATCH  /api/admin/ai/conversations/:id
DELETE /api/admin/ai/conversations/:id
DELETE /api/admin/ai/conversations
POST   /api/admin/ai/conversations/:id/messages/stream
POST   /api/admin/ai/insights/generate
```

首轮会话能力包括：新建、切换、自动命名、手动重命名、删除单个会话、清空全部记录。删除和清空由前端二次确认，后端仍需执行权限校验和事务保护。首轮不包含搜索、收藏和导出。

## 6. SSE 协议

流式消息接口返回：

```http
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

事件类型：

- `meta`：会话、消息和模型元数据；
- `delta`：新增文本片段；
- `done`：正常完成；
- `error`：可展示的错误码和信息。

Nginx 对该接口关闭代理缓冲。后端扩展 DeepSeek client，使用 `stream=true`，逐行解析上游数据并及时 flush。

用户消息立即入库。AI 消息先以 `streaming` 状态创建，后端在内存中累积片段；正常完成时一次更新为 `completed`，客户端取消或连接断开时保存已有内容并标记 `aborted`，上游失败时标记 `failed`。不得按每个 token 单独写数据库。

请求上下文取消必须向上游传递。前端停止生成后，后端取消 DeepSeek 请求并释放资源。

## 7. 上下文策略

- 系统提示词只由后端生成；
- 从 PostgreSQL 读取最近若干轮完整对话；
- 超过模型上下文限制时保留最近消息；
- 旧消息仍保存在数据库并可查看；
- 不依赖浏览器提交历史来恢复会话；
- 上下文选择和消息顺序必须有单元测试。

## 8. SQLite 到 PostgreSQL 迁移

新增独立一次性命令：

```text
cmd/migrate-sqlite-to-postgres
```

迁移命令不在服务启动时自动执行。连接凭据从服务器受保护的环境文件读取，不通过命令行打印或提交。

### 8.1 安全默认值

- 支持 `--dry-run`；
- 目标库非空时默认拒绝；
- 源 SQLite 只读；
- 不删除 SQLite、WAL 或 SHM；
- 输出表名、数量和校验结果，不输出隐私内容；
- 正式迁移使用事务，失败整体回滚。

### 8.2 搬迁要求

- 按外键依赖顺序迁移全部现有业务表；
- 保留原主键；
- 恢复文章、标签、评论、用户和访问记录等关联；
- 迁移后重置 PostgreSQL 序列；
- 上传文件继续保存在文件系统，仅校验数据库记录和文件路径；
- 新增 AI 表结构，首次迁移时允许为空。

### 8.3 校验

- 逐表记录数；
- 最小、最大和重复主键；
- 外键与多对多关系；
- PostgreSQL 序列；
- 文章正文、slug、站点设置等关键字段摘要或哈希；
- 核心公开和管理 API 冒烟测试；
- AI 会话创建、删除和 SSE 响应测试。

## 9. 生产切换与回滚

生产迁移采用短暂停写维护窗口：

1. 暂停自动部署；
2. 进入维护状态；
3. 备份 SQLite、上传目录和环境配置；
4. 导出现有 PostgreSQL 备份；
5. 执行 dry-run；
6. 执行正式迁移和校验；
7. 切换 `BLOG_DATABASE_DRIVER` 与 `BLOG_DATABASE_DSN`；
8. 部署兼容版本；
9. 启动服务，执行健康检查和核心冒烟测试；
10. 成功后解除维护并恢复自动部署。

迁移或校验失败时不切换配置。切换后健康检查失败时，在解除维护前恢复上一版二进制和 SQLite 配置。SQLite 及备份保留观察期，不立即删除。

## 10. GitHub Actions

现有 backend workflow 已提供 PostgreSQL service，继续复用并补充：

- AI 会话 CRUD 集成测试；
- SSE delta/done/error 测试；
- 请求取消与 aborted 状态测试；
- PostgreSQL 事务和级联关系测试；
- SQLite 测试夹具到 PostgreSQL 的迁移测试；
- 记录数、关联和序列校验测试。

生产历史迁移使用独立 `workflow_dispatch`，绑定 production environment 审批。普通 master push 只测试、构建、部署程序，不自动搬迁历史数据。

## 11. 安全与可观测性

- 所有 AI 管理接口要求管理员 JWT；
- 不在日志中记录完整对话、DSN、密钥或用户隐私；
- 日志记录 request ID、conversation ID、message ID、耗时、结果状态和上游错误类别；
- 对输入长度、会话归属、并发生成和请求频率设置限制；
- SSE 错误使用稳定错误码，前端不展示上游原始敏感响应；
- PostgreSQL 和上传目录都纳入备份，任一部分失败则整个备份任务失败。

## 12. 验收标准

- dashboard 普通数据不等待 AI；
- DeepSeek 不可用时后台其他功能仍可操作；
- SSE 首个片段到达后立即显示；
- 停止生成会取消上游请求并保存部分内容；
- 会话可新建、恢复、重命名、删除和清空；
- 刷新或换设备后可从 PostgreSQL 恢复历史；
- SQLite 全部业务数据迁移并通过数量、关联、序列和抽样校验；
- 后端 CI 在 PostgreSQL 下通过；
- 生产迁移可审批、可审计、可回滚。
