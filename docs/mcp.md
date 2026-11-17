# MCP 接口

[文档首页](/docs/) · [HTTP API](/docs/api) · [Go SDK](/docs/sdk) · [Markdown 原文](/docs/mcp.md)

## 地址与认证

统一地址为 `POST /mcp`，接收 JSON-RPC 2.0。MCP 覆盖全部 28 项 HTTP API 操作：身份、scope 查询、项目、环境、配置、变更提案、API Key、用户和系统设置。项目由每次工具调用的 `project` 参数选择，可以在同一连接中管理多个项目。

```http
Authorization: Bearer cc_REPLACE_WITH_YOUR_KEY
Content-Type: application/json
Accept: application/json, text/event-stream
MCP-Protocol-Version: 2025-06-18
```

API Key 的每项 scope 绑定项目与操作权限，`project: "*"` 覆盖所有项目（包括未来创建的项目）。用户管理和系统设置权限只能配置在 `*` 范围。权限逐项授权，写入不自动包含读取或删除。完整权限表和 Key 创建方式见 [HTTP API](/docs/api)。

浏览器会话也可调用管理工具，每次 POST 必须携带 `X-CSRF-Token`。普通用户只能管理自己的项目和 Key；用户管理、系统设置需要管理员身份。`get_project` 与 `list_values` 对应 SDK 接口，要求 API Key 或 MCP OAuth 令牌。显式提供 Authorization 后不会回退到浏览器会话。外部 Agent 可使用 API Key 或下述 OAuth 登录，不需要 Cookie 或 CSRF Token。旧的 `Authorization: ApiKey …` 已不再接受，现有 Key 的值无需更换。

## OAuth 授权登录

配置现有的 `CC_OAUTH_ISSUER`、`CC_OAUTH_CLIENT_ID`、`CC_OAUTH_CLIENT_SECRET` 后，MCP OAuth 自动启用；上游身份提供商的回调仍为 `CC_PUBLIC_URL/oauth/callback`。`CC_PUBLIC_URL` 必须是客户端可访问的站点地址，正式环境使用 HTTPS。反向代理部署时也应配置公开的 HTTPS 地址。无需在上游身份提供商为每个 MCP 客户端单独创建应用。

在支持 OAuth 的 MCP 客户端添加 `https://你的域名/mcp` 并选择 OAuth 授权。未登录时，客户端会打开现有身份提供商的登录页；随后 ConfigCenter 显示客户端名称、回调地址、授权账号和权限说明，用户允许后才签发授权码。已登录的用户也必须为每次授权明确确认，拒绝会返回 `access_denied`。

实现依据 [MCP 2025-06-18 授权规范](https://modelcontextprotocol.io/specification/2025-06-18/basic/authorization)，提供以下端点：

| 端点                                            | 用途                                                     |
| ----------------------------------------------- | -------------------------------------------------------- |
| `GET /.well-known/oauth-protected-resource/mcp` | 受保护资源及授权服务器发现；无 `/mcp` 后缀的地址也可使用 |
| `GET /.well-known/oauth-authorization-server`   | 授权、注册、令牌和撤销端点及能力声明                     |
| `POST /oauth/register`                          | 动态注册 public client，返回 `client_id`                 |
| `GET /oauth/authorize`                          | 登录与用户授权，支持 `response_type=code`                |
| `POST /oauth/token`                             | 授权码换取令牌，或刷新令牌                               |
| `POST /oauth/revoke`                            | 撤销一组授权的全部访问与刷新令牌                         |

未认证的 `/mcp` 请求返回 HTTP 401，并在 `WWW-Authenticate: Bearer` 中给出 `resource_metadata` 和 `scope="mcp"`。发现文档与端点地址使用配置的 `CC_PUBLIC_URL`，不使用请求中的 Host。

客户端注册请求为 JSON，例如：

```json
{
  "client_name": "My MCP client",
  "redirect_uris": ["http://127.0.0.1:4567/callback"],
  "grant_types": ["authorization_code", "refresh_token"],
  "response_types": ["code"],
  "token_endpoint_auth_method": "none"
}
```

支持无客户端密钥的 public client。注册时按原值保存 `redirect_uris`，不校验 URI 格式、协议、主机、端口、路径、查询参数或 fragment，也不配置客户端回调白名单。`redirect_uris` 仍须是包含 1–10 项的字符串数组，注册请求体上限为 16 KiB。

客户端可注册多个回调，授权时选择其中任意一个。授权请求必须与某个已注册地址精确匹配，兑换授权码时的 `redirect_uri` 必须与该次授权请求一致。用户确认后，页面直接使用服务端返回的回调地址跳转，不再自行解析或校验 URI；回调格式与客户端处理能力由客户端负责。

不支持 implicit flow、密码授权或 `client_credentials`。PKCE、用户确认、CSRF、客户端绑定和资源绑定保持生效。

授权请求包含 `client_id`、`redirect_uri`、`response_type=code`、`scope=mcp`、`resource=CC_PUBLIC_URL/mcp`、`code_challenge` 和 `code_challenge_method=S256`。客户端应生成并验证 `state`。授权响应保留 `state`，同时返回 `iss`。PKCE verifier 长度为 43–128，必须由 RFC 7636 允许的字符组成。

令牌端点使用 `application/x-www-form-urlencoded`。授权码兑换参数为 `grant_type=authorization_code`、`client_id`、`code`、`redirect_uri`、`code_verifier` 和相同的 `resource`。刷新时发送 `grant_type=refresh_token`、`client_id`、`refresh_token` 和相同的 `resource`。两者不发送客户端密钥或 HTTP Basic 认证。

访问令牌使用 `Authorization: Bearer <access_token>`，有效期最多 1 小时；仅在注册时声明 `refresh_token` grant 的客户端会收到刷新令牌。授权总有效期为 30 天，每次刷新都替换访问令牌和刷新令牌。重复兑换授权码或使用旧刷新令牌会撤销该授权的全部令牌，客户端应串行刷新，并保存最新的令牌对。授权码有效期为 5 分钟，登录和确认请求有效期为 10 分钟。

撤销时向 `/oauth/revoke` 提交表单 `client_id` 和 `token`（访问令牌或刷新令牌均可）；未知令牌同样返回 HTTP 200。服务端只保存令牌摘要，授权数据保存在现有数据库中。

OAuth 的 `mcp` scope 代表以账号身份访问 MCP：普通用户限于自己的项目和 Key，管理员还可管理用户与系统设置。每次请求重新检查账号当前角色，删除账号或撤销授权会使令牌失效。OAuth 令牌只用于 `/mcp`，不能直接调用 `/api/v1`；上游身份提供商的 access token 或 id token 不能用作 MCP 凭据。需要按项目和具体操作限制客户端时，使用 scope API Key。

## 连接与初始化

服务采用无状态 HTTP POST，直接返回 JSON。没有 SSE 推送、会话 ID 或 JSON-RPC 批处理；GET 和 DELETE `/mcp` 在认证后返回 HTTP 405。协议版本为 `2025-06-18`，显式发送其他 `MCP-Protocol-Version` 返回 HTTP 400。带有 `Origin` 的请求必须匹配 `CC_PUBLIC_URL` 的源，否则返回 HTTP 403。传输和工具结构分别参考 [MCP 传输规范](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports) 和 [工具规范](https://modelcontextprotocol.io/specification/2025-06-18/server/tools)。

示例中的 `CC_URL`（无尾部斜杠）和 `CC_KEY` 由调用方安全提供，不是服务端配置项：

```sh
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $CC_KEY" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  "$CC_URL/mcp" \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"config-agent","version":"1.0.0"}}}'
```

初始化响应：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-06-18",
    "capabilities": { "tools": { "listChanged": false } },
    "serverInfo": { "name": "configcenter", "version": "1.0.0" }
  }
}
```

随后发送 `{"jsonrpc":"2.0","method":"notifications/initialized"}`。合法且不带 `id` 的通知返回 HTTP 202、无响应体，也不执行工具。需要执行工具时必须携带字符串或数字 `id`。

## 方法与工具发现

| 方法                             | 参数                | 结果                             |
| -------------------------------- | ------------------- | -------------------------------- |
| `initialize`                     | 见上例              | 协议版本、能力、服务端信息       |
| `ping`                           | 无                  | 空对象                           |
| `tools/list`                     | 无                  | 可用工具及各工具的 `inputSchema` |
| `tools/call`                     | `name`、`arguments` | 工具执行结果                     |
| `resources/list`、`prompts/list` | 无                  | 空列表                           |

```json
{ "jsonrpc": "2.0", "id": 2, "method": "tools/list" }
```

发现结果按当前凭据的 scope 过滤：在任意授权项目有对应权限时，该项目工具就会出现；调用时仍按实际 `project` 校验。需要多个权限的工具必须在同一个项目具备全部权限，不会将不同项目的权限合并。直接调用未列出的工具同样会检查权限。scope 修改后立即影响后续请求，客户端可重新调用 `tools/list` 刷新列表。

所有参数位于 `params.arguments`，未知字段、缺失必填字段、错误类型或枚举值会被拒绝。下表除特别说明外均为字符串；`id` 是正整数，`approve` 和 `allow_registration` 是布尔值，`scopes` 是数组。

| 工具                 | 必填参数                                  | 可选参数                                     | scope                                  |
| -------------------- | ----------------------------------------- | -------------------------------------------- | -------------------------------------- |
| `get_identity`       | 无                                        | 无                                           | 已认证                                 |
| `get_key_scopes`     | 无                                        | 无                                           | 已认证                                 |
| `list_projects`      | 无                                        | `view`（`mine` 对浏览器和 OAuth 有效）       | `projects:read`                        |
| `create_project`     | `slug`、`name`                            | `description`                                | `*` 的 `projects:write`                |
| `update_project`     | `project`、`name`                         | `description`                                | `projects:write`                       |
| `delete_project`     | `project`                                 | 无                                           | `projects:delete`                      |
| `list_environments`  | `project`                                 | 无                                           | `environments:read`                    |
| `create_environment` | `project`、`slug`、`name`                 | 无                                           | `environments:write`                   |
| `delete_environment` | `project`、`environment`                  | 无                                           | `environments:delete`                  |
| `list_config`        | `project`、`environment`                  | `prefix`、`keys`                             | `config:read`                          |
| `get_config`         | `project`、`environment`、`key`           | 无                                           | `config:read`                          |
| `set_config`         | `project`、`environment`、`key`、`value`  | `description`、`new_key`                     | `config:write`                         |
| `set_configs`        | `project`、`environment`、`items`         | —                                            | `config:write`                         |
| `delete_config`      | `project`、`environment`、`key`           | 无                                           | `config:delete`                        |
| `list_proposals`     | `project`                                 | `status`（`pending`、`applied`、`rejected`） | `proposals:read`                       |
| `propose_config`     | `project`、`environment`、`key`、`action` | `value`（`set` 时必填）、`reason`            | `proposals:write`                      |
| `review_proposal`    | `project`、`id`、`approve`                | 无                                           | `proposals:review`                     |
| `list_keys`          | 无                                        | `project`                                    | `keys:read`                            |
| `create_key`         | `name`、`scopes`                          | `expires_at`（RFC 3339）                     | `keys:write`                           |
| `update_key`         | `id`、`name`、`scopes`                    | 无                                           | `keys:write`                           |
| `rotate_key`         | `id`                                      | 无                                           | `keys:write`                           |
| `delete_key`         | `id`                                      | 无                                           | `keys:write`                           |
| `list_users`         | 无                                        | 无                                           | `*` 的 `users:read`                    |
| `update_user_role`   | `id`、`role`（`user` 或 `admin`）         | 无                                           | `*` 的 `users:write`                   |
| `delete_user`        | `id`                                      | 无                                           | `*` 的 `users:delete`                  |
| `get_settings`       | 无                                        | 无                                           | `*` 的 `settings:read`                 |
| `update_settings`    | `allow_registration`                      | 无                                           | `*` 的 `settings:write`                |
| `get_project`        | `project`                                 | 无                                           | `projects:read` 与 `environments:read` |
| `list_values`        | `project`、`environment`                  | `prefix`、`keys`                             | `config:read`                          |

工具执行与 HTTP API 使用同一套处理逻辑和权限检查。创建项目会自动创建 `production` 环境。删除项目或环境会级联清理其内容。Key 管理只能授予或操作自身 scope 覆盖的权限，不能通过创建、编辑或轮换扩大权限。用户不能通过自己的会话或 Key 删除自己的账号或将自己降级；删除其他用户会撤销其会话和 Key，并将项目转交给调用者账号。

## 调用与结果

跨项目写入时在每次调用中选择项目：

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "set_config",
    "arguments": {
      "project": "billing",
      "environment": "production",
      "key": "db.pool",
      "value": "20",
      "description": "数据库连接池大小"
    }
  }
}
```

`value` 必须为字符串，也允许空字符串。数字、布尔值或 JSON 需先序列化。省略 `description` 会将描述设为空字符串；提供 `new_key` 可重命名。项目和环境使用 slug，配置键支持 `/`，传入原始字符串即可，无需手动 URL 编码。

结果的 `structuredContent` 包含 HTTP API 响应字段和 `http_status`。`content[0].text` 是相同对象的 JSON 字符串。无响应体的成功操作返回 `success: true`，例如删除配置：

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "content": [{ "type": "text", "text": "{\"http_status\":204,\"success\":true}" }],
    "structuredContent": { "http_status": 204, "success": true },
    "isError": false
  }
}
```

创建带独立项目权限的 Key：

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "tools/call",
  "params": {
    "name": "create_key",
    "arguments": {
      "name": "worker",
      "scopes": [
        { "project": "billing", "permissions": ["config:read"] },
        { "project": "shipping", "permissions": ["config:write"] }
      ]
    }
  }
}
```

创建结果包含 `key` 元数据和一次性完整 `token`；轮换返回新 `token`，原 token 立即失效。编辑 scope 保留 token，`scopes: []` 撤销该 Key 的所有操作权限。Key 列表不返回 token 或摘要。

## 变更提案

`propose_config` 的 `action` 只能是 `set` 或 `delete`。`set` 必须提供 `value`（允许空字符串），`delete` 不需要。成功返回提案字段，包括 `id`、`environment`、`key`、`action`、`value`、`reason`、`status: "pending"` 和时间戳；此时配置尚未修改。

有 `proposals:review` 的凭据可调用 `review_proposal`，传入 `project`、提案 `id` 和 `approve`。`true` 立即应用提案，`false` 拒绝提案。也可在管理界面的「变更提案」页审核。`list_proposals` 不传 `status` 时返回所有状态。

每次提交都会创建新提案，没有幂等键。网络结果不确定时先查询列表，避免重复创建。

## 错误处理

请求体上限为 1 MiB。依次检查 HTTP 状态、顶层 `error` 和 `result.isError`，HTTP 200 并不代表工具成功。

| 情况                                             | 响应                                                                                                                    |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------- |
| 缺少凭据，或 Key 错误、过期、已吊销              | HTTP 401                                                                                                                |
| 浏览器 CSRF 错误或 Origin 不允许                 | HTTP 403                                                                                                                |
| 不支持的协议请求头                               | HTTP 400                                                                                                                |
| JSON 无法解析、超过大小限制或多个 JSON 值        | HTTP 400，`error.code = -32700`                                                                                         |
| JSON-RPC 版本、方法字段或 ID 非法                | HTTP 200，`error.code = -32600`                                                                                         |
| 未知方法或工具                                   | HTTP 200，`error.code = -32601`                                                                                         |
| 工具参数不符合 schema                            | HTTP 200，`error.code = -32602`                                                                                         |
| 内部编码错误                                     | HTTP 200，`error.code = -32603`                                                                                         |
| 工具权限不足、资源不存在、业务校验失败或并发冲突 | HTTP 200，`result.isError = true`；`structuredContent.error` 描述错误，`http_status` 为对应的 400、403、404、409 等状态 |

服务端内部故障只返回通用错误。为调用设置超时，保留请求 ID 匹配响应；不要把密钥、配置值或提案内容写入诊断日志。认证和参数错误需先修正请求再重试。
