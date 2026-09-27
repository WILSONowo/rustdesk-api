# 账号 API

基于上游项目，新增邮箱验证注册、管理员审核、邮件通知、注册查重与验证码，并保留个人和共享通讯录。客户端下载链接和服务器配置导入码也可以在后台管理。

项目包含三个配套仓库：

| 仓库 | 用途 |
| --- | --- |
| [rustdesk-api](https://github.com/WILSONowo/rustdesk-api) | 账号 API、邮件和数据管理 |
| [rustdesk-api-web](https://github.com/WILSONowo/rustdesk-api-web) | 管理后台和用户页面 |
| [rustdesk-server](https://github.com/WILSONowo/rustdesk-server) | ID 服务端的 API 登录握手修复 |

API 和 Web 配套使用，ID／中继服务可以单独部署。账号审核管理登录和通讯录权限；远控仍使用客户端的密码或确认授权。

部署见 [ACCOUNT_V1.md](ACCOUNT_V1.md)，发信配置见 [EMAIL_SETUP.md](EMAIL_SETUP.md)。
