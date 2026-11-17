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

浏览器会话也可调用管理工具，每次 POST 必须携带 `X-CSRF-Token`。普通用户只能管理自己的项目和 Key；用户管理、系统设置需要管理员身份。`get_project` 与 `list_values` 对应 SDK 接口，要求 API Key。显式提供 Authorization 后不会回退到浏览器会话。外部 Agent 使用 API Key，不需要 Cookie 或 CSRF Token。

## 连接与初始化

服务采用无状态 HTTP POST，直接返回 JSON。没有 SSE 推送、会话 ID 或 JSON-RPC 批处理；GET 和 DELETE `/mcp` 返回 HTTP 405。协议版本为 `2025-06-18`，显式发送其他 `MCP-Protocol-Version` 返回 HTTP 400。带有 `Origin` 的请求必须匹配 `CC_PUBLIC_URL` 的源，否则返回 HTTP 403。传输和工具结构分别参考 [MCP 传输规范](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports) 和 [工具规范](https://modelcontextprotocol.io/specification/2025-06-18/server/tools)。

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
| `list_projects`      | 无                                        | `view`（`mine` 仅对浏览器有效）              | `projects:read`                        |
| `create_project`     | `slug`、`name`                            | `description`                                | `*` 的 `projects:write`                |
| `update_project`     | `project`、`name`                         | `description`                                | `projects:write`                       |
| `delete_project`     | `project`                                 | 无                                           | `projects:delete`                      |
| `list_environments`  | `project`                                 | 无                                           | `environments:read`                    |
| `create_environment` | `project`、`slug`、`name`                 | 无                                           | `environments:write`                   |
| `delete_environment` | `project`、`environment`                  | 无                                           | `environments:delete`                  |
| `list_config`        | `project`、`environment`                  | `prefix`、`keys`                           | `config:read`                          |
| `get_config`         | `project`、`environment`、`key`           | 无                                           | `config:read`                          |
| `set_config`         | `project`、`environment`、`key`、`value`  | `description`、`new_key`                     | `config:write`                         |
| `set_configs`        | `project`、`environment`、`items`          | —                                            | `config:write`                         |
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
| `list_values`        | `project`、`environment`                  | `prefix`、`keys`                           | `config:read`                          |

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
