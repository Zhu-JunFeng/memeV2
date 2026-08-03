# GitHub Actions CI/CD 部署

## 背景

V2 项目需要从 GitHub 的干净分支构建、测试并部署到生产服务器，避免依赖本地脏工作区手工打包。

## 变更

- 新增 `.github/workflows/ci.yml`，在 PR 和 `main` 推送时执行后端测试、前端测试和前端构建。
- 新增 `.github/workflows/deploy-main.yml`，在 `main` 推送或手动触发时执行测试、构建 Linux 后端二进制、构建前端 dist，并部署到生产服务器。
- 部署流程使用 GitHub Secrets 中的服务器密码连接生产机，上传 release 到 `/data/solana-scalper-v2/releases`，再备份并替换当前后端二进制和前端静态文件。
- 生产配置文件、数据库密码、钱包私钥、API Key 不进入仓库和构建产物，继续保留在生产服务器。

## GitHub Secrets

需要在 GitHub Actions Secrets 中配置：

- `PROD_HOST`：生产服务器地址，例如 `47.251.140.83`
- `PROD_USER`：生产服务器用户，例如 `root`
- `PROD_PASSWORD`：生产服务器登录密码
- `PROD_BACKEND_DIR`：后端目录，例如 `/data/solana-scalper-v2/backend`
- `PROD_FRONTEND_DIR`：前端目录，例如 `/data/solana-scalper-v2/frontend`
- `PROD_HEALTH_URL`：公网健康检查地址，例如 `http://47.251.140.83/api/health`

## 验证

- 后端测试：`cd backend && mkdir -p .tmp/go-build-cache && GOCACHE=$PWD/.tmp/go-build-cache go test ./...`
- 前端测试和构建：`cd frontend && npm test && npm run build`
- 部署后验证：`systemctl is-active solana-meme-backtest-v2.service` 与 `/api/health`
