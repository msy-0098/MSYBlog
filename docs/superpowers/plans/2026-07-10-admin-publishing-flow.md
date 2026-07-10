# 管理端文章发布闭环 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让管理员完成 Markdown 编辑预览、正文与封面图片上传、分类标签设置、发布下线以及公开详情验证。

**Architecture:** 复用现有文章 CRUD、上传接口和公开 `MarkdownRenderer`。编辑器内部管理编辑/预览模式与光标插图，列表快捷发布通过先取完整文章再调用现有 PUT 接口完成，避免新增后端状态专用接口。

**Tech Stack:** Vue 3、TypeScript、Element Plus、Axios、Markdown It、Vitest、Vue Test Utils、pnpm；Go 路由测试用于确认公开联动

---

## File Map

- Modify: `../frontend/src/components/admin/PostEditor.vue` - 预览、正文插图、封面预览和字段校验。
- Modify: `../frontend/src/components/admin/PostEditor.test.ts` - 编辑器交互测试。
- Modify: `../frontend/src/views/admin/AdminPostEditView.vue` - 加载错误、保存结果和详情联动。
- Create: `../frontend/src/views/admin/AdminPostEditView.test.ts` - 创建、发布和错误保留测试。
- Modify: `../frontend/src/views/admin/AdminPostsView.vue` - 发布、下线和查看文章快捷操作。
- Create: `../frontend/src/views/admin/AdminPostsView.test.ts` - 状态操作测试。
- Modify: `../frontend/src/styles/global.css` - 编辑器稳定布局和预览样式。
- Modify: `internal/router/admin_content_routes_test.go` - 发布、隐藏、分类标签和公开详情联动回归。

### Task 1: 用后端测试锁定发布联动契约

- [ ] **Step 1: 扩展管理内容路由测试**

在 `TestAdminContentCreatesPublishedPostVisibleToVisitors` 中创建包含分类、两个标签、封面和 Markdown 正文的草稿，依次验证：

```go
update := performJSONRequest(engine, http.MethodPut, "/api/admin/posts/"+itoa(postID), map[string]any{
    "title": "发布闭环文章",
    "slug": "publishing-workflow",
    "summary": "发布流程回归",
    "content": "# 标题\n\n![封面](/uploads/test.png)",
    "cover": "/uploads/test.png",
    "status": "published",
    "categoryId": categoryID,
    "tagIds": []uint{tagOneID, tagTwoID},
}, adminToken)
```

公开详情应返回分类、两个标签、封面和正文；再次更新为 `hidden` 后，公开详情应为 404。

- [ ] **Step 2: 运行测试确认当前契约**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/router -run TestAdminContentCreatesPublishedPostVisibleToVisitors -v
```

Expected: 若配置了 PostgreSQL 测试 DSN则 PASS；否则明确 SKIP。若失败，只修正真实后端契约问题，不新增状态专用接口。

- [ ] **Step 3: 运行后端完整测试**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./...
```

Expected: PASS 或仅数据库集成测试按设计 SKIP。

- [ ] **Step 4: 如测试文件有改动，提交并推送后端**

```powershell
git status --short
git add internal/router/admin_content_routes_test.go
git commit -m "test: cover article publishing lifecycle"
git push origin master
git push gitee master
```

Expected: 后端两个远端同步测试提交；若现有测试已完整覆盖且无需改动，则不创建空提交。

### Task 2: 用失败测试定义 Markdown 预览和图片插入

- [ ] **Step 1: 扩展 `PostEditor.test.ts`**

增加以下测试：

```ts
it('switches between markdown editing and rendered preview', async () => {
  const wrapper = mountEditor()
  await wrapper.get('[data-test="post-content-textarea"]').setValue('## Preview title')
  await wrapper.get('[data-test="post-preview-mode"]').trigger('click')

  expect(wrapper.findComponent(MarkdownRenderer).props('content')).toBe('## Preview title')
  expect(wrapper.find('[data-test="post-content-textarea"]').exists()).toBe(false)
})
```

模拟 `uploadAdminImage` 返回 `/uploads/body.png`，设置 textarea 选区后触发文件变化，断言正文包含 `![body](/uploads/body.png)`。再验证封面上传后出现图片预览和清除按钮。

- [ ] **Step 2: 增加字段校验测试**

标题、slug、正文或分类为空时不触发 `submit`，并通过 `validation-error` 显示具体字段提示。

- [ ] **Step 3: 运行测试确认失败**

```powershell
pnpm exec vitest run src/components/admin/PostEditor.test.ts
```

Expected: FAIL，当前没有预览、正文上传、封面预览和组件级校验状态。

### Task 3: 实现编辑器完整能力

- [ ] **Step 1: 增加编辑/预览分段控制**

```ts
const editorMode = ref<'edit' | 'preview'>('edit')
```

模板使用两个带 `aria-pressed` 的按钮和稳定标识 `post-edit-mode`、`post-preview-mode`。预览区复用：

```vue
<MarkdownRenderer v-if="editorMode === 'preview'" :content="form.content" />
<textarea v-else ref="contentTextarea" v-model="form.content" data-test="post-content-textarea" />
```

- [ ] **Step 2: 实现正文图片上传和光标插入**

```ts
function insertMarkdownImage(path: string, name: string) {
  const markdown = `![${name.replace(/\.[^.]+$/, '')}](${path})`
  const textarea = contentTextarea.value
  const start = textarea?.selectionStart ?? form.content.length
  const end = textarea?.selectionEnd ?? start
  form.content = `${form.content.slice(0, start)}${markdown}${form.content.slice(end)}`
}
```

正文和封面都调用现有 `uploadAdminImage`；上传按钮显示独立 loading，文件 input 每次完成后清空。

- [ ] **Step 3: 实现封面预览与移除**

只有 `form.cover` 非空时显示真实 `<img>`，使用稳定尺寸和 `object-fit: cover`；移除按钮把 `form.cover` 置空。

- [ ] **Step 4: 实现提交前校验**

按标题、slug、正文、分类顺序返回首个中文错误；slug 使用 `/^[a-z0-9]+(?:-[a-z0-9]+)*$/`。无错误时才 emit 完整 `AdminPostPayload`。

- [ ] **Step 5: 运行编辑器测试**

```powershell
pnpm exec vitest run src/components/admin/PostEditor.test.ts
```

Expected: PASS。

### Task 4: 用失败测试定义保存后详情联动

- [ ] **Step 1: 创建 `AdminPostEditView.test.ts`**

覆盖已发布文章保存成功后保留返回对象，并提供：

```ts
expect(wrapper.get('[data-test="view-published-post"]').attributes('href'))
  .toBe('/posts/publishing-workflow')
```

另测加载失败显示重试按钮，保存失败后 `PostEditor` 的 `initialValue` 和当前输入不被重置。

- [ ] **Step 2: 运行测试确认失败**

```powershell
pnpm exec vitest run src/views/admin/AdminPostEditView.test.ts
```

Expected: FAIL，当前保存后直接返回列表且没有可查看入口。

- [ ] **Step 3: 实现保存结果区域**

`savePost` 接收 API 返回的 `AdminPost`。发布成功后不立即离开，显示“文章已发布”、查看公开文章和返回列表；草稿或隐藏状态保存后返回列表。加载失败显示 `retry-editor-load`。

- [ ] **Step 4: 运行编辑页测试**

```powershell
pnpm exec vitest run src/views/admin/AdminPostEditView.test.ts src/components/admin/PostEditor.test.ts
```

Expected: PASS。

### Task 5: 用失败测试定义列表发布和下线

- [ ] **Step 1: 创建 `AdminPostsView.test.ts`**

草稿行点击“发布”时先调用 `getAdminPost(id)`，再断言：

```ts
expect(updateAdminPost).toHaveBeenCalledWith(7, {
  title: post.title,
  slug: post.slug,
  summary: post.summary,
  content: post.content,
  cover: post.cover,
  status: 'published',
  categoryId: post.categoryId,
  tagIds: post.tags.map((tag) => tag.id),
  publishedAt: post.publishedAt
})
```

已发布行点击“下线”应提交 `hidden`；已发布行还应存在 `/posts/:slug` 查看入口。

- [ ] **Step 2: 运行测试确认失败**

```powershell
pnpm exec vitest run src/views/admin/AdminPostsView.test.ts
```

Expected: FAIL，当前只有编辑和删除。

- [ ] **Step 3: 实现完整载荷转换函数**

```ts
function toPayload(post: AdminPost, status: AdminPostStatus): AdminPostPayload {
  return {
    title: post.title,
    slug: post.slug,
    summary: post.summary,
    content: post.content,
    cover: post.cover,
    status,
    categoryId: post.categoryId,
    tagIds: post.tags.map((tag) => tag.id),
    publishedAt: post.publishedAt
  }
}
```

快捷操作前弹出确认框，成功后刷新当前页；失败只提示错误，不修改本地行状态。

- [ ] **Step 4: 运行列表测试**

```powershell
pnpm exec vitest run src/views/admin/AdminPostsView.test.ts
```

Expected: PASS。

### Task 6: 完整前端验证与浏览器检查

- [ ] **Step 1: 运行完整测试和构建**

```powershell
pnpm test
pnpm build
```

Expected: 全部测试通过且生产构建成功。

- [ ] **Step 2: 启动本地前端**

```powershell
pnpm dev --host 127.0.0.1
```

Expected: Vite 给出本地 URL，保持进程运行直到浏览器检查完成。

- [ ] **Step 3: 使用浏览器检查桌面和移动视口**

检查管理端编辑/预览切换不引起布局错位，长标题和长 slug 不溢出，封面预览不遮挡操作，正文上传按钮可用，发布成功入口能进入真实详情。分别检查约 1440x900 和 390x844。

- [ ] **Step 4: 停止开发服务器并确认无产物误入**

```powershell
git status --short
```

Expected: 不含 `dist/`、`node_modules/`、截图、上传文件、日志或缓存。

### Task 7: 提交并推送管理端发布闭环

- [ ] **Step 1: 检查差异**

```powershell
git status --short
git diff --check
```

Expected: 只包含计划列出的前端源码、样式和测试。

- [ ] **Step 2: 提交并推送**

```powershell
git add src/components/admin/PostEditor.vue src/components/admin/PostEditor.test.ts src/views/admin/AdminPostEditView.vue src/views/admin/AdminPostEditView.test.ts src/views/admin/AdminPostsView.vue src/views/admin/AdminPostsView.test.ts src/styles/global.css
git commit -m "feat: complete article publishing workflow"
git push origin master
git push gitee master
```

Expected: frontend 的 GitHub 和 Gitee `master` 同步成功。
