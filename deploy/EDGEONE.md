# EdgeOne HTTPS 回源部署

示例源站：`203.0.113.10`（文档保留地址，请替换），API 与网页一起运行，Nginx 提供 HTTPS 回源。证书使用 Let’s Encrypt HTTP-01 验证和 Certbot 自动续期，无需 DNS API Key。

- 源码目录：`/opt/rustdesk-accounts/rustdesk-api` 和相邻的 `rustdesk-api-web`。
- Compose：`rustdesk-api/deploy/compose.edgeone.yaml`，项目名 `rustdesk-accounts`。
- 源站地址：`203.0.113.10`，最终回源协议 **HTTPS**，回源端口 **443**。API 仅监听 `127.0.0.1:21114`。
- 加速域名及回源 Host：`remote.example.com`。
- 用户访问协议：HTTPS，边缘证书由 EdgeOne 配置；源站证书由本机 Certbot 管理，两段证书分别配置。
- API 设置：`https://remote.example.com`，不加 `/api`。
- ID / 中继服务器仍为 `rd.example.com:21116` / `rd.example.com:21117`，不走这个 HTTP CDN。

## EdgeOne 控制台

1. 添加加速域名 `remote.example.com`，源站类型选择 IP，填 `203.0.113.10`。
2. 证书签发后固定 HTTPS 回源，端口 `443`；回源 Host 和 SNI 填 `remote.example.com`，启用源站证书校验。
3. 按 EdgeOne 提供的记录配置 DNS，启用客户端 HTTPS 证书及 HTTP 跳转 HTTPS。
4. 初次部署可将此域名全部设置为**不缓存**。至少 `/api/*`、`/_admin/` 和 `/_admin/index.html` 不缓存，禁止强制缓存 API。程序已返回 `Cache-Control: no-store, private`，但 CDN 强制缓存规则可以覆盖它。
5. `/api/*` 保留 Authorization、api-token、Content-Type、查询参数及 POST 请求体，不使用需要浏览器执行 JavaScript 的挑战页，否则桌面客户端可能无法登录。
6. 给注册 `/api/admin/user/register` 和登录 `/api/login`、`/api/admin/login` 配置按真实客户端 IP 的频率限制。Nginx 也有限流，但未配置 CDN 地址信任时，其计数是按直接连接的 CDN 节点 IP 汇总，应用不会无条件信任任意来源的 X-Forwarded-For。
7. 优先匹配 `/.well-known/acme-challenge/*`：**不缓存、HTTP 回源到 203.0.113.10:80、绕过挑战页**。保留此规则用于自动续期；该路径需能通过公网 HTTP 80 到达源站验证文件，不能被全局跳转规则反复重定向。

首次签发前先安装 Nginx、Certbot 和 curl，并让 Nginx 在 80 端口提供 `/.well-known/acme-challenge/`（root 为 `/var/www/rustdesk-acme`）。可先只安装示例中的第一个 HTTP server 块，验证路径以外暂时返回 503；此时不要安装引用尚不存在证书的 HTTPS server 块。完成第 7 步和 DNS 后执行下面的脚本。脚本会自动生成随机验证文件，并将 HTTPS 模板中的示例域名替换为 DOMAIN。

```bash
cd /opt/rustdesk-accounts/rustdesk-api/deploy
DOMAIN=remote.example.com sh enable-https.sh
```

该脚本先确认公网验证文件确实来自本源站，再签发证书、启用 Nginx HTTPS、安装续期重载钩子并执行续期演练。公网验证未通过时不会申请证书。全新数据库的初始管理员密码由应用启动日志提供；请妥善保管并首次登录后修改。

续期后通过 `/etc/letsencrypt/renewal-hooks/deploy/reload-nginx` 自动验证并重载 Nginx；`certbot.timer` 负责定期检查。未提供邮箱时无证书邮件提醒。

后台：`https://remote.example.com/_admin/`；注册：`https://remote.example.com/_admin/#/register`。

## 服务管理

```bash
cd /opt/rustdesk-accounts/rustdesk-api/deploy
docker compose -f compose.edgeone.yaml ps
docker compose -f compose.edgeone.yaml logs --tail=80 api
docker compose -f compose.edgeone.yaml up -d --build
```

数据在 `rustdesk-accounts_account-data`，日志在 `rustdesk-accounts_account-runtime`；不要执行 `down -v`。生产部署请使用独立数据卷。

`accounts-origin` 模式在应用内禁止 API 缓存，将匿名 `/api/heartbeat`、`/api/sysinfo*`、`/api/audit/*` 返回 404，并将 API 请求体限制为 2 MiB。这保留了第一版账号服务的范围，因此在线状态/匿名设备上报可能不完整。账户、个人及共享通讯录仍可使用。

源站 80/443 可公开访问，21114 仅供本机反代使用，不宣称已限制为 CDN 独占。配置 CDN 后，可以进一步按该站点的 EdgeOne 回源 IP 列表收紧入口，同时保留 ACME 验证链路；不要在没有真实回源地址清单时猜测放行网段。

参考：[EdgeOne 缓存规则](https://edgeone.ai/zh/document/54213)、[回源协议配置](https://edgeone.ai/document/54205)。
