# Fork 源码构建与发布

本分支包含开放注册与管理员审核、邮箱验证/找回密码/通知队列、注册查重及验证码、个人/共享通讯录、客户端下载链接和配置导入码，以及配套界面优化。

## 三个独立仓库

| 仓库 | 分支 | 职责 |
| --- | --- | --- |
| WILSONowo/rustdesk-api | codex/account-review-v1 | 账号 API、邮件队列、数据库迁移 |
| WILSONowo/rustdesk-api-web | codex/account-review-v1 | 管理后台与用户网页 |
| WILSONowo/rustdesk-server | codex/api-secure-handshake | 官方 1.1.16 hbbs 的 API 登录 TCP 握手兼容 |

API 与 Web 应配套检出；服务端独立构建部署。客户端仓库 `rustdesk` 不属于本次修改。
握手兼容不校验 API token，也不强制所有远控用户登录，详见服务端的 `API_HANDSHAKE.md`。

## 从源码构建

在同一父目录克隆 Web/API，切换上表分支，再在 API 目录运行：

```sh
docker compose -f deploy/compose.local.yaml up -d --build
```

本地地址：`http://127.0.0.1:21124/_admin/`。第一次启动会在日志中生成管理员初始密码。邮件默认关闭，需要自己的 SMTP 才能完成新用户注册；已有用户登录不受影响。

生产部署参阅 [ACCOUNT_V1.md](ACCOUNT_V1.md)、[EMAIL_SETUP.md](EMAIL_SETUP.md)、[deploy/EDGEONE.md](deploy/EDGEONE.md)。将示例域名 `remote.example.com`、`rd.example.com` 和示例 IP 换成自己的地址，并在不受 Git 跟踪的 `deploy/.env` 填写配置。

建议发布时记录两个仓库的完整 commit SHA，用提交号检出后构建 `Dockerfile.source`，避免不同时间的分支内容混用。原 Go module 路径保持上游名称，不影响 fork 构建。

## 验证与发布边界

`.github/workflows/verify.yml` 仅运行测试，不发布 Docker 镜像、不创建 Release、不部署服务器。旧上游发布流程移至 `.github/legacy-workflows/`，因为它们包含上游前端地址和镜像目标，不适合直接用于本 fork。

测试与构建需要 Go 1.24、CGO/C 编译器、Node.js 22，或 Docker。Web 执行 `npm ci && npm test && npm run build`。后端执行 `go test -mod=readonly ./...`；Redis 集成测试仅在设置 `REDIS_TEST_ADDR` 时运行，必须指向可写的临时测试实例，不能使用生产 Redis。CI 自带隔离 Redis 并运行全部测试。

提交前检查 `git diff --cached`；只提交源码、测试、示例和文档。真实 `.env`、数据库、私钥、构建产物、运维日志和 `artifacts/` 留在本地。源码沿用原 MIT 许可；服务端仓库保留原 AGPL-3.0 许可，不合并改换许可证。

需要推送时分别推送对应分支，PR 的目标选择自己的 fork。不要使用 `--force`，不要把旧上游发布工作流直接移回启用。

## 已知边界

- 当前邮箱/注册流程主要通过 SQLite 测试；未声明已完成其他数据库的发布验收。
- CDN 后真实 IP 需按可信代理链配置；示例不会信任任意转发头，未配置时多个用户可能共享来源 IP 配额。
- `accounts-origin` 关闭匿名遥测；在线状态/设备上报可能不完整。
- 下载配置目前提供链接，不自动拉取或托管客户端二进制。
- 本地测试不替代双方官方客户端的实际远控与邮件投递验收。
