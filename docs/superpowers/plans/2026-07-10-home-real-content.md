# 首页真实内容闭环 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让首页文章、精选文章、分类、项目和站点配置分别使用真实接口，并在局部失败、空数据和重试时保持其余内容可用。

**Architecture:** 保留现有公开 API，不增加聚合接口。`HomeView.vue` 为四组数据维护独立请求状态，首页内容组件只负责渲染状态并通过 `retry` 事件触发对应请求。

**Tech Stack:** Vue 3、TypeScript、Axios、Vitest、Vue Test Utils、pnpm

---

## File Map

- Modify: `../frontend/src/views/HomeView.vue` - 拆分站点、文章、分类和项目请求状态与重试函数。
- Modify: `../frontend/src/views/HomeView.test.ts` - 覆盖局部失败、空状态和单区重试。
- Modify: `../frontend/src/components/home/FeaturedEssay.vue` - 精选区错误重试。
- Modify: `../frontend/src/components/home/LatestPosts.vue` - 最新文章区错误重试。
- Modify: `../frontend/src/components/home/CategoryCloud.vue` - 分类区错误重试。
- Modify: `../frontend/src/components/home/FeaturedProjects.vue` - 项目区错误重试。

### Task 1: 建立前端可执行基线

- [ ] **Step 1: 安装锁文件对应依赖**

Run from `../frontend`:

```powershell
pnpm install --frozen-lockfile
```

Expected: 依赖安装成功，`pnpm-lock.yaml` 不发生变化，`node_modules/` 保持忽略状态。

- [ ] **Step 2: 运行首页现有测试**

```powershell
pnpm exec vitest run src/views/HomeView.test.ts
```

Expected: 现有 2 条首页测试通过。

- [ ] **Step 3: 检查仓库状态**

```powershell
git status --short
```

Expected: 不出现 `node_modules/`、`dist/`、`.vite/` 或锁文件改动。

### Task 2: 用失败测试定义局部错误与重试

- [ ] **Step 1: 在 `HomeView.test.ts` 增加局部失败测试**

新增测试核心断言：

```ts
it('keeps successful homepage sections visible when projects fail', async () => {
  vi.mocked(getProjects).mockRejectedValueOnce(new Error('项目接口暂不可用'))
  const wrapper = mountHome()

  await flushPromises()

  expect(wrapper.text()).toContain('Real API Post')
  expect(wrapper.text()).toContain('Real Category')
  expect(wrapper.text()).toContain('项目接口暂不可用')
  expect(wrapper.find('[data-test="retry-projects"]').exists()).toBe(true)
})
```

- [ ] **Step 2: 增加单区重试测试**

```ts
it('retries only the failed projects request', async () => {
  vi.mocked(getProjects)
    .mockRejectedValueOnce(new Error('项目接口暂不可用'))
    .mockResolvedValueOnce([makeProject('Recovered Project')])
  const wrapper = mountHome()
  await flushPromises()

  await wrapper.get('[data-test="retry-projects"]').trigger('click')
  await flushPromises()

  expect(getProjects).toHaveBeenCalledTimes(2)
  expect(getPosts).toHaveBeenCalledTimes(1)
  expect(getCategories).toHaveBeenCalledTimes(1)
  expect(wrapper.text()).toContain('Recovered Project')
})
```

测试文件中提取 `mountHome()` 和 `makeProject()`，避免每条测试重复挂载配置。

- [ ] **Step 3: 运行测试确认失败**

```powershell
pnpm exec vitest run src/views/HomeView.test.ts
```

Expected: FAIL，原因是当前 `Promise.all` 会让全部内容失败，且不存在 `retry-projects`。

### Task 3: 拆分首页请求状态

- [ ] **Step 1: 在 `HomeView.vue` 定义独立状态**

用以下结构替换共享的 `contentLoading` 和 `contentError`：

```ts
const postLoading = ref(true)
const postError = ref('')
const categoryLoading = ref(true)
const categoryError = ref('')
const projectLoading = ref(true)
const projectError = ref('')

async function loadPosts() {
  postLoading.value = true
  postError.value = ''
  featuredPostOverride.value = null
  try {
    const postPage = await getPosts({ page: 1, pageSize: 6 })
    posts.value = postPage.list
    await loadFeaturedFallback(postPage.list)
  } catch (error) {
    posts.value = []
    postError.value = toMessage(error, '文章加载失败')
  } finally {
    postLoading.value = false
  }
}
```

`loadCategories()` 和 `loadProjects()` 使用同样状态结构；项目成功后仍只保留前三项。`onMounted` 使用 `Promise.allSettled([loadSite(), loadPosts(), loadCategories(), loadProjects()])` 启动并发任务。

- [ ] **Step 2: 保留精选文章独立回退**

```ts
async function loadFeaturedFallback(latest: PostSummary[]) {
  const slug = profile.value.featuredPostSlug
  if (!slug || latest.some((post) => post.slug === slug)) return

  try {
    const result = await getPosts({ slug, page: 1, pageSize: 1 })
    featuredPostOverride.value = result.list[0] ?? null
  } catch {
    featuredPostOverride.value = null
  }
}
```

- [ ] **Step 3: 向各内容组件传递对应状态和重试事件**

```vue
<LatestPosts
  :posts="latestPosts"
  :loading="postLoading"
  :error="postError"
  @retry="loadPosts"
/>
<CategoryCloud
  :categories="categories"
  :loading="categoryLoading"
  :error="categoryError"
  @retry="loadCategories"
/>
<FeaturedProjects
  :projects="projects"
  :loading="projectLoading"
  :error="projectError"
  @retry="loadProjects"
/>
```

- [ ] **Step 4: 运行测试确认数据隔离部分仍因按钮缺失而失败**

```powershell
pnpm exec vitest run src/views/HomeView.test.ts
```

Expected: 成功内容能够保留，但重试按钮断言仍失败。

### Task 4: 为四个首页区增加可访问的重试操作

- [ ] **Step 1: 每个内容组件声明重试事件**

```ts
const emit = defineEmits<{ retry: [] }>()
```

- [ ] **Step 2: 将错误状态改为信息和按钮组合**

各组件使用唯一测试标识，例如项目区：

```vue
<div v-else-if="error" class="state-line error-line" role="alert">
  <span>{{ error }}</span>
  <button data-test="retry-projects" type="button" @click="emit('retry')">重新加载</button>
</div>
```

其他标识为 `retry-featured-post`、`retry-posts` 和 `retry-categories`。按钮沿用全局文本按钮样式，不新增卡片容器。

- [ ] **Step 3: 运行首页测试**

```powershell
pnpm exec vitest run src/views/HomeView.test.ts
```

Expected: PASS，包含成功、精选回退、局部失败和单区重试测试。

- [ ] **Step 4: 运行前端完整测试与构建**

```powershell
pnpm test
pnpm build
```

Expected: 所有 Vitest 测试通过，Vite 生产构建成功。

### Task 5: 提交首页闭环

- [ ] **Step 1: 检查禁止提交项**

```powershell
git status --short
git diff --check
```

Expected: 只包含上述首页源码和测试；不包含 `node_modules/`、`dist/`、缓存、日志或本地配置。

- [ ] **Step 2: 提交并推送**

```powershell
git add src/views/HomeView.vue src/views/HomeView.test.ts src/components/home/FeaturedEssay.vue src/components/home/LatestPosts.vue src/components/home/CategoryCloud.vue src/components/home/FeaturedProjects.vue
git commit -m "feat: complete homepage content states"
git push origin master
git push gitee master
```

Expected: frontend 的 GitHub 与 Gitee `master` 都包含该提交。
