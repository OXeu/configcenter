# Configcenter 接入文档

将配置集中存储在项目的不同环境中，通过 HTTP API 或 Go SDK 读取。

- [HTTP API 接入](/docs/api)：鉴权、环境发现、读取配置与错误处理。
- [Go SDK 接入](/docs/sdk)：初始化、类型转换、快照和自动刷新。
- [MCP 接口](/docs/mcp)：连接、工具参数、读取、写入、提案与错误处理。
- Markdown 原文：[首页](/docs/index.md)、[API](/docs/api.md)、[SDK](/docs/sdk.md)、[MCP](/docs/mcp.md)。Agent 可直接 GET 这些地址，无需登录或执行 JavaScript。

## 准备工作

1. 部署并启动 Configcenter，通过 OAuth 登录管理界面。
2. 创建项目（自动创建 `production` 环境），在目标环境中添加配置。
3. 在账户菜单的「密钥」页面创建 API Key，选择项目范围和权限；读取配置选择 `config:read` 即可。完整 token 只在创建和轮换时返回，请保存到应用的秘密配置中。
4. 将服务根地址、密钥和环境 slug 传给调用方。只授权一个具体项目时 Agent REST 可省略项目参数；多项目或 `*` scope 调用 Agent REST 时通过 `?project=` 指定项目，MCP 始终通过路径指定。

以下示例使用 `https://config.example.com`、`production` 环境与 `db.pool` 配置项。替换为真实部署信息。生产调用使用 HTTPS，不要把密钥放进前端代码、URL、日志或版本库。

## 选择接入方式

任意语言可使用只读 REST API；Go 应用可使用独立 SDK 模块。SDK 不提供配置写入、密钥管理或项目管理接口。

每把 Key 都可独立配置项目、环境、配置、提案和密钥管理的操作权限。普通用户只能授权自己的项目，管理员可配置 `*` 范围。通过 [HTTP API](/docs/api) 或页面编辑 scope，无需更换 token。

需要修改配置时使用管理界面；Agent 自动化可使用 [MCP 接口](/docs/mcp) `POST /mcp`，携带同样的 `Authorization: Bearer <token>`。MCP 也支持通过现有身份提供商进行 OAuth 授权登录，配置与流程见 [MCP 文档](/docs/mcp)。MCP 是 JSON-RPC 2.0，覆盖完整 API，包括项目、环境、配置、Key、用户、系统设置和提案审核；每次调用通过参数选择项目，操作受 scope 限制。不要向只读 REST 路径发送 PUT 或 POST。
