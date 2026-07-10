# Blog CI/CD 设计说明

日期：2026-07-10

## 目标

为 `backend/` 和 `frontend/` 两个独立 Git 仓库建立轻量 CI/CD：Pull Request 只执行验证，合并到 `master` 后由 GitHub Actions 构建并通过 SSH 部署到 Ubuntu 服务器。

## 方案

- GitHub `origin` 作为唯一 CI/CD 触发源。
- Gitee `gitee` 作为代码镜像，按项目规则同步推送，但不重复部署。
- 前端使用 pnpm、Node.js 22，执行 `pnpm test` 和 `pnpm build`。
- 后端使用 Go 1.25.5，启动 PostgreSQL 16 service，执行 `go test ./...` 和 Linux amd64 构建。
- 前端使用 `/var/www/blog/releases/<commit-sha>` 版本目录和 `current` 软链接切换版本。
- 后端上传到 `/home/ubuntu/blog/backend/blog-server`，重启 `blog.service`，再检查 `/api/site`。
- 生产密钥不进入仓库；由服务器 `/etc/blog/blog.env` 提供运行时配置，Actions 只保存部署 SSH 凭证。

## 发布触发

- `pull_request`：执行前端和后端验证，不部署。
- `push` 到 `master`：验证成功后上传构建产物并部署。
- 使用 `production` environment，为后续增加人工审批保留入口。

## 服务器前置条件

- `ubuntu` 用户持有 `/var/www/blog` 和 `/home/ubuntu/blog` 的部署权限。
- `blog.service` 使用 `EnvironmentFile=/etc/blog/blog.env`。
- `ubuntu` 可以免密码执行指定的 `systemctl restart blog.service` 和 `systemctl is-active blog.service`。
- Actions Secrets 配置 `DEPLOY_HOST`、`DEPLOY_PORT`、`DEPLOY_USER`、`DEPLOY_SSH_KEY`、`DEPLOY_KNOWN_HOSTS`。

## 不在本次范围内

- 不自动生成或提交生产密码、数据库 DSN、SMTP 密码、AI API Key。
- 不同时启用 Gitee CI，避免一次提交重复部署。
- 不修改现有业务代码和数据库迁移逻辑。
