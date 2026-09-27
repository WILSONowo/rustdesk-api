# 账号与通讯录第一版

基于 `WILSONowo/rustdesk-api` 与 `WILSONowo/rustdesk-api-web`，工作分支均为 `feature/api-rebuild`。保留上游 MIT 许可证和版权声明。

## 行为

- 开放网页注册，密码 8–32 字符。所有自行注册的账号先进入「待审核」，注册成功不会签发登录令牌。
- 管理员在「系统 → 用户管理」筛选「待审核」，点击「筛选」，再在右侧「注册审核」列选择「通过」或「拒绝」。本地入口为 `http://127.0.0.1:21124/_admin/#/user/index`。只有待审核账号显示审核按钮，已通过账号不会显示。通过后可登录网页及 RustDesk 桌面客户端；拒绝后账号为禁用状态。重复审核会提示刷新。
- 移除页面顶部的历史标签栏，使用左侧菜单切换页面。
- 状态：`1` 启用、`2` 禁用／拒绝、`3` 待审核。旧的 `app.register-status` 保留兼容，但本分支不允许它跳过审核。`app.register: false` 仍可关闭公开注册。
- 保留个人通讯录、通讯录集合和按用户／用户组共享功能；只读、读写、完全控制权限沿用上游。
- 第一版使用本地账号密码。新增 OAuth 账号同样待审核，中央令牌签发也校验数据库中当前账号状态；LDAP 的企业目录同步策略不在本次范围内，部署默认关闭。不要在没有单独验证策略前开启 LDAP／第三方身份提供商。
- 修复服务端命令接口的管理员权限缺失、网页退出未撤销令牌、禁用后重新启用恢复旧会话等问题。无 JWT 密钥时使用 256 位随机令牌；登录写入数据库失败不会返回令牌。

**账号审核控制登录和通讯录访问，不强制远控双方登录，也不限制账号只能远控指定设备。** 远控连接仍由既有 `hbbs/hbbr` 和被控端密码／确认授权处理。

「服务端命令」默认关闭，页面显示说明，不会自动连接 API 容器的 `127.0.0.1:21115/21117`。只有使用兼容命令接口的 hbbs/hbbr、且该接口与 API 处于同一网络空间时，才可设置 `admin.server-commands-enabled: true`（环境变量 `RUSTDESK_API_ADMIN_SERVER_COMMANDS_ENABLED=true`）。修改 ID／中继服务器公网地址不会连接此命令接口，也不要为此把命令接口暴露到公网。未启用不影响账号、通讯录和远控。

## 本地构建

示例 API 域名为 `remote.example.com`，部署前请替换为自己的域名。本地测试使用 `http://127.0.0.1:21124`，不会修改电脑上已安装 RustDesk 的网络设置。

目录相邻，且两个仓库都使用这一版的修改：

```text
workspace/
  rustdesk-api/
  rustdesk-api-web/
```

后端测试需要 CGO 和 SQLite 的 C 编译环境，可用 Docker：

```bash
cd rustdesk-api
docker run --rm -v "$PWD:/src" -w /src golang:1.24-bookworm \
  go test -mod=readonly ./http/... ./service ./utils ./lib/jwt
docker build -f Dockerfile.source \
  --build-context frontend=../rustdesk-api-web \
  -t rustdesk-accounts:v1-local .
```

前端：在 `rustdesk-api-web` 中执行 `npm ci && npm run build`。源码镜像会自动构建前端和后端，不需要使用上游预编译发行包。

启动独立的本地测试服务（Windows PowerShell 同样适用）：

```bash
cd rustdesk-api
docker compose -f deploy/compose.local.yaml up -d --build
docker compose -f deploy/compose.local.yaml logs --tail=80 api
```

打开 `http://127.0.0.1:21124/_admin/`。本地卷使用 `rustdesk-accounts-local` 项目前缀，与部署卷隔离。网页注册、审核、登录、手动维护及共享通讯录可直接测试；若要在本地 RustDesk 客户端验证同步，API 地址填 `http://127.0.0.1:21124`，ID／中继服务器仍使用已有可连接配置。本地 Compose 中的默认 `127.0.0.1:21116/21117` 只是占位，可通过 `RUSTDESK_ID_SERVER`／`RUSTDESK_RELAY_SERVER`／`RUSTDESK_PUBLIC_KEY` 环境变量换成现有配置。另一台电脑的 `127.0.0.1` 指向它自己，不能据此访问本机服务。

## 配套仓库与发布

Web 与 API 分别维护，发布时需使用配套提交。请参阅 [fork 构建说明](FORK_RELEASE.md)。
`artifacts/`、运行数据库、密钥和部署 `.env` 不属于源码发布内容。

## 部署到服务器

现有 `/opt/rustdesk` 的 `hbbs/hbbr` 继续运行。API 和网页可以部署在另一台服务器上，下面的步骤在准备承载 API 的服务器执行。账号服务使用独立 Compose 项目及独立数据卷，不挂载远控服务器私钥。`rd.example.com` 指向原远控服务器，`remote.example.com` 指向 API 服务器；若配置 AAAA，也应指向对应服务器可访问的 IPv6。

1. 将两个修改后的仓库放在服务器的同一父目录下。**本地修改尚未推送时，直接在服务器 clone GitHub 默认分支不会得到这些改动。** 使用本地源码归档，或在分支推送后分别检出 `feature/api-rebuild`。

首次部署，且两个分支均已推送后：

```bash
mkdir -p /opt/rustdesk-accounts
cd /opt/rustdesk-accounts
git clone --branch feature/api-rebuild https://github.com/WILSONowo/rustdesk-api.git
git clone --branch feature/api-rebuild https://github.com/WILSONowo/rustdesk-api-web.git
```

2. 配置 API：

```bash
cd rustdesk-api/deploy
cp .env.example .env
chmod 600 .env
nano .env
docker compose up -d --build
docker compose ps
docker compose logs --tail=80 api
```

`.env` 设置：

| 变量 | 内容 |
| --- | --- |
| `RUSTDESK_ID_SERVER` | 现有服务器 IP／域名加 `:21116` |
| `RUSTDESK_RELAY_SERVER` | 现有服务器 IP／域名加 `:21117` |
| `RUSTDESK_API_URL` | `https://remote.example.com`，不加 `/api` |
| `RUSTDESK_PUBLIC_KEY` | 现有 `id_ed25519.pub` 内容；不是 `id_ed25519` 私钥 |
| `TRUSTED_PROXY_IPS` | 精确的受信任反向代理地址；未知时留空，不能填 `0.0.0.0/0` 或 `::/0` |

IPv6 字面量带端口时使用 `[IPv6地址]:21116`／`[IPv6地址]:21117`。建议客户端使用解析正确的域名。

3. 服务只发布 `127.0.0.1:21114`，通过服务器上的 Nginx 提供 HTTPS。`deploy/nginx.conf.example` 提供反代、请求大小限制和注册／登录限流样例。替换域名及证书路径，先取得有效证书，再执行 `nginx -t` 并重载。此样例适用于宿主机 Nginx；若 Nginx 自身也在容器内，必须调整上游网络地址。
4. 放行 API 服务器的 TCP 443；若采用 HTTP 验证申请证书，也需要 TCP 80。打开 `https://remote.example.com/_admin/`。全新数据库的管理员账号为 `admin`，随机初始密码由上游初始化逻辑写入启动日志；立即登录并修改密码。不要把包含密码的日志发到公共场所。
5. 用户访问 `https://remote.example.com/_admin/#/register` 注册。管理员审核通过后，用户在 RustDesk「设置 → 网络」中保留原 ID／中继服务器与公钥，另填 **API 服务器**为 `https://remote.example.com`，然后在客户端登录。

临时无域名时，可使用 SSH 隧道测试网页，不必向公网开放明文 API：

```bash
# 在自己的电脑执行，服务器已有 SSH 登录方式即可
ssh -L 21114:127.0.0.1:21114 root@你的服务器
```

然后打开 `http://127.0.0.1:21114/_admin/`。多台客户端的公网联调待 HTTPS 配置完成后再进行。

### 通讯录操作

用户的「我的地址簿」存放个人设备；「我的地址簿集合」中新建集合，再通过集合的规则页面授权指定用户或用户组。共享内容放入该集合，接收方在客户端刷新共享通讯录。建议首次测试给另一个账号只读权限，确认可见且无法修改，再按需要开放写权限。

### 数据与更新

- 数据库保存在 `rustdesk-accounts_account-data` 卷；应用日志和缓存保存在 `rustdesk-accounts_account-runtime` 卷。
- 升级前停止 API 并备份两个卷，保存当时的两个仓库提交和镜像。SQLite 不要在写入期间仅复制主数据库文件。
- 回滚时使用备份对应的镜像与数据；**不要执行 `docker compose down -v`，该命令会删除数据卷**。
- 旧 API 数据库的已启用用户不会自动重新审核；原先禁用用户仍是状态 2，可由管理员手工启用。此前泄露的令牌需在后台主动撤销。
- 原仓库的旧发布工作流仍面向上游发行流程；这一版请使用 `Dockerfile.source` 构建，确保修改后的前端被打包进去。

## 验证范围

`http/router/accounts_test.go` 使用真实路由、中间件与临时 SQLite，覆盖注册待审核、伪造角色字段、密码确认、网页／客户端登录拒绝、管理员审批、重复审批、拒绝注册、状态筛选、普通用户越权拒绝、个人通讯录隔离、共享只读与撤销、退出失效、禁用会话撤销、最后一个管理员保护及登录数据库失败回滚。

这不是对整个上游项目的安全审计。Nginx 样例关闭了不在第一版范围内的匿名审计／系统信息／心跳上传接口，因此相关在线状态、设备发现和审计展示可能不完整；账号登录和手动通讯录维护不依赖这些上传。没有把这些上传数据当成可信审计日志。

真实 RustDesk 客户端版本兼容性、公网 IPv4／IPv6、Clash 路由及两个终端间的通讯录同步还需要在部署后联调。API 测试通过不代表这些公网链路已经验证。
