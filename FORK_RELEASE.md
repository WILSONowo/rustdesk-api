# 账号 API fork

本版本包含开放注册与管理员审核、邮箱验证/找回密码/通知队列、注册查重及验证码、个人/共享通讯录、客户端下载链接和配置导入码，以及配套界面优化。

## 三个独立仓库

| 仓库 | 职责 |
| --- | --- |
| [WILSONowo/rustdesk-api](https://github.com/WILSONowo/rustdesk-api) | 账号 API、邮件队列、数据库迁移 |
| [WILSONowo/rustdesk-api-web](https://github.com/WILSONowo/rustdesk-api-web) | 管理后台与用户网页 |
| [WILSONowo/rustdesk-server](https://github.com/WILSONowo/rustdesk-server) | 官方 1.1.16 hbbs 的 API 登录 TCP 握手兼容 |

API 与 Web 应配套检出；服务端独立构建部署。客户端仓库 `rustdesk` 不属于本次修改。
握手兼容不校验 API token，也不强制所有远控用户登录，详见服务端的 [API_HANDSHAKE.md](https://github.com/WILSONowo/rustdesk-server/blob/master/API_HANDSHAKE.md)。
