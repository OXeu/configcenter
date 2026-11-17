# HTTP API 接入

[文档首页](/docs/) · [Go SDK](/docs/sdk) · [MCP 接口](/docs/mcp) · [Markdown 原文](/docs/api.md)

## 批量读写配置

批量读取使用 `GET /api/v1/projects/:project/environments/:env/items?keys=certcenter.server&keys=certcenter.providers`，需要 `config:read`。SDK 路径 `/api/v1/agent/environments/:env/values` 同样接受重复的 `keys` 参数。返回 `items` 数组和 `missing_keys` 数组；不存在的键不会使整个请求失败。不传 `keys` 时仍按原有列表方式读取，可用 `prefix` 筛选；`keys` 与 `prefix` 不能同时使用。

批量写入使用 `PUT /api/v1/projects/:project/environments/:env/items`，只需要对应项目的 `config:write`。请求示例：

```json
{
  "items": [
    { "key": "feature.enabled", "value": "false" },
    { "key": "db.pool", "value": "10", "description": "连接池大小" }
  ]
}
```

每批接受 1–500 个不重复的键，写入请求体上限为 1 MiB。值必须是字符串，允许空字符串；省略 description 时按空字符串保存。全部验证通过后，在一个事务中创建或更新所有项，任何失败都会回滚整批。成功返回 `204`，不返回配置值。项目和环境须预先存在。原有单项 GET、PUT 和 DELETE 接口保持兼容。

MCP 的 `list_config` 和 `list_values` 接受 `keys` 字符串数组，`set_configs` 接受上述 `items` 数组。SDK 提供 `ReadMany` 和 `PutMany`；`EnsureDefaults` 先批量读，再批量写入缺失项，不使用独立初始化 API。远端首次初始化应由一个实例完成，读写之间不提供跨请求并发保护。

## 密钥字段与读取权限

创建或编辑配置时可设置 `secret: true`。未传 `secret` 的单项和批量写入保留已有标记；新增字段默认不是密钥。取消标记需要 `config:read-secrets`，MCP 不允许取消。单项更新已有密钥时可省略 `value` 以保留原值并修改键名或描述；不要把掩码作为新值提交。批量写入仍要求每项提供 `value`。

读取权限分为两个独立 scope：

| scope                 | 行为                         |
| --------------------- | ---------------------------- |
| `config:read`         | 普通读取，密钥始终返回掩码   |
| `config:read-secrets` | 允许通过专用接口读取密钥明文 |

所有普通管理 API、SDK 读取接口和 MCP 工具都返回 `secret`、`masked` 标记，密钥的 `value` 为掩码，例如 `sk-****x123`。超过 8 个字符的值只保留前 3、后 4 个字符；8 个字符以内（包括空值）统一为 `****`。即使凭据同时拥有特权 scope，普通接口仍只返回掩码。写入响应、提案列表及审核响应遵循同一规则。字段改为密钥后，已有提案也会持续受保护，字段重命名或删除不会公开历史提案值。

明文只通过以下显式授权接口返回，响应使用 `Cache-Control: no-store`：

| 方法 | 路径                                                            | 权限                                                                 |
| ---- | --------------------------------------------------------------- | -------------------------------------------------------------------- |
| POST | `/api/v1/projects/:project/environments/:env/items/:key/reveal` | `config:read-secrets`                                                |
| POST | `/api/v1/projects/:project/proposals/:id/reveal`                | `config:read-secrets` 和 `proposals:read`                            |
| GET  | `/api/v1/agent/environments/:env/values/reveal`                 | `config:read-secrets`；支持 `project`、`prefix` 或重复的 `keys` 参数 |

这些接口不提供 MCP 工具；MCP 即使持有特权 scope 也不能调用。浏览器用户仍按项目所有权和角色授权，点击「查看」后才发送带 CSRF 的明文请求；隐藏、刷新或切换页面后清除已查看的值。编辑弹窗可以保留原密钥、授权查看，或直接输入替代值。服务端不会通过普通页面数据预加载明文。

## 认证与 scope

请求携带 `Authorization: Bearer cc_…`。所有 API Key 使用 scope 权限模型。旧的 `ApiKey` 鉴权 scheme 不再接受，已有 Key 的值无需更换。MCP 的 OAuth 访问令牌仅用于 `/mcp`，不能直接调用这里的 REST API。

每把 Key 的 `scopes` 是一组“项目范围 + 操作权限”。同一把 Key 可以只读取项目 A，同时写入项目 B；`project: "*"` 表示所有项目（包括以后创建的项目）。权限需要逐项授权，写入不自动包含读取或删除。

```json
{
  "name": "billing-worker",
  "scopes": [
    { "project": "billing", "permissions": ["config:read"] },
    { "project": "shipping", "permissions": ["config:write"] }
  ]
}
```

这里的 project 使用 slug，不是展示名称。每个项目只能出现一次，每项 scope 必须有至少一项权限；未知或重复权限会被拒绝。`scopes: []` 可以保存一把没有访问权限的 Key，省略 `scopes` 则返回 HTTP 400。

浏览器登录后，在账户菜单的「密钥」页面 `/keys` 创建、编辑、轮换或吊销 Key。普通用户可管理自己创建的 Key，授权范围限于自己拥有的项目；管理员可管理所有 Key，并选择所有项目。编辑 scope 保留原 token，后续请求立即使用新权限。所有 scope 覆盖目标项目的全部环境。

## 可配置权限

| 权限                  | 允许的操作                                                   |
| --------------------- | ------------------------------------------------------------ |
| `projects:read`       | 列出授权范围内的项目、读取项目元数据                         |
| `projects:write`      | 修改项目名称和描述；范围为 `*` 时可创建项目                  |
| `projects:delete`     | 删除项目及其环境、配置和提案                                 |
| `environments:read`   | 读取环境列表                                                 |
| `environments:write`  | 创建环境                                                     |
| `environments:delete` | 删除环境及其配置                                             |
| `config:read`         | 读取配置列表或单个配置项，密钥返回掩码                       |
| `config:read-secrets` | 通过专用授权接口读取配置明文                                 |
| `config:write`        | 创建、更新或重命名配置                                       |
| `config:delete`       | 删除配置                                                     |
| `proposals:read`      | 列出变更提案                                                 |
| `proposals:write`     | 提交变更提案                                                 |
| `proposals:review`    | 批准或拒绝提案；批准会应用提案中的配置写入或删除             |
| `keys:read`           | 列出授权范围内、权限未超出当前 Key 的 API Key 元数据         |
| `keys:write`          | 创建、编辑、轮换或吊销授权范围内的 API Key，不能超出自身权限 |

父资源删除会级联删除内容，因此 `projects:delete`、`environments:delete` 也包含对应的配置清理能力。`keys:write` 不能用于提权：调用方必须对目标 Key 的每项项目范围都拥有 `keys:write`，且目标 Key 的全部权限必须是调用方权限的子集；创建或修改后的 scope 也要满足这个约束。只有 `keys:read` 不会返回完整 token。

用户管理和系统设置 scope 只能配置在 `project: "*"`，指定具体项目会返回 HTTP 400。普通用户不能授予这些权限。

| 权限             | 允许的操作                               |
| ---------------- | ---------------------------------------- |
| `users:read`     | 列出用户                                 |
| `users:write`    | 修改用户角色                             |
| `users:delete`   | 删除用户及其会话和 Key，将项目转交调用者 |
| `settings:read`  | 读取系统设置                             |
| `settings:write` | 修改系统设置                             |

## 读取配置

以下命令假设调用环境已安全提供 `CC_URL`（无尾部斜杠）和 `CC_KEY`（完整 token）。这些是调用方变量，不是服务端配置项。

只授权一个具体项目的 Key 可省略项目参数；多项目 Key 或包含 `*` 的 Key 必须用 `?project=billing` 指定项目，否则返回 HTTP 400。

```sh
curl --fail-with-body --silent --show-error --get \
  -H "Authorization: Bearer $CC_KEY" \
  --data-urlencode 'project=billing' \
  --data-urlencode 'prefix=db.' \
  "$CC_URL/api/v1/agent/environments/production/values"
```

此请求需要 billing 项目的 `config:read`。响应示例：

```json
{
  "project": "billing",
  "environment": "production",
  "items": [
    {
      "key": "db.pool",
      "value": "20",
      "description": "数据库连接池大小",
      "updated_at": "2026-01-01T00:00:00Z"
    }
  ]
}
```

`value` 始终是字符串，数字、布尔值或 JSON 由调用方解析。无匹配项时 `items` 为空数组。此接口没有分页或推送订阅，调用方可定时刷新。

`GET /api/v1/agent/project?project=billing` 返回 `project` 和 `environments`，需要同时拥有 `projects:read` 与 `environments:read`。环境 slug 和配置键作为路径段时需 URL 编码，项目和 prefix 查询参数也需要编码。

## 项目管理 API

浏览器会话和 API Key 共用以下接口。浏览器修改请求需要 `X-CSRF-Token` 和项目管理权限；使用 API Key 时不需要 Cookie 或 CSRF Token。显式提供 Authorization 后不会回退到浏览器会话。

| 方法   | 路径                                                     | scope / 请求体                                                                                                |
| ------ | -------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/v1/projects`                                       | `projects:read`；只返回 scope 允许读取的项目                                                                  |
| POST   | `/api/v1/projects`                                       | `*` 范围的 `projects:write`；`slug`、`name`、可选 `description`                                               |
| PATCH  | `/api/v1/projects/:project`                              | `projects:write`；`name`、`description`                                                                       |
| DELETE | `/api/v1/projects/:project`                              | `projects:delete`                                                                                             |
| GET    | `/api/v1/projects/:project/environments`                 | `environments:read`                                                                                           |
| POST   | `/api/v1/projects/:project/environments`                 | `environments:write`；`slug`、`name`                                                                          |
| DELETE | `/api/v1/projects/:project/environments/:env`            | `environments:delete`                                                                                         |
| GET    | `/api/v1/projects/:project/environments/:env/items`      | `config:read`；可选 `?prefix=`                                                                                |
| GET    | `/api/v1/projects/:project/environments/:env/items/:key` | `config:read`；读取单个配置项                                                                                 |
| PUT    | `/api/v1/projects/:project/environments/:env/items/:key` | `config:write`；`value` 必填字符串，可选 `description`、`key`（重命名）                                       |
| DELETE | `/api/v1/projects/:project/environments/:env/items/:key` | `config:delete`                                                                                               |
| GET    | `/api/v1/projects/:project/proposals`                    | `proposals:read`；可选 `?status=pending`                                                                      |
| POST   | `/api/v1/projects/:project/proposals`                    | `proposals:write`；`environment`、`key`、`action`（`set` / `delete`）、`value`（`set` 时必填）、可选 `reason` |
| POST   | `/api/v1/projects/:project/proposals/:id/review`         | `proposals:review`；`approve` 布尔值                                                                          |

创建项目会自动创建 `production` 环境，所有者及操作归属记录为 Key 的创建者。删除项目会移除 Key 对该项目的 scope，保留其他项目的授权；最后一项 scope 移除后，该 Key 没有访问权限。重新创建相同 slug 的项目不会恢复旧的项目授权；`*` 范围仍覆盖新项目。

```sh
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $CC_KEY" \
  -H 'Content-Type: application/json' \
  -X PUT "$CC_URL/api/v1/projects/shipping/environments/production/items/db.pool" \
  --data '{"value":"20","description":"数据库连接池大小"}'
```

## API Key 管理

| 方法   | 路径                      | 行为                                                                                                  |
| ------ | ------------------------- | ----------------------------------------------------------------------------------------------------- |
| GET    | `/api/v1/keys`            | 列表；可选 `?project=billing` 过滤项目，包括覆盖该项目的 `*` scope                                    |
| POST   | `/api/v1/keys`            | 创建；`name`、`scopes` 必填，可选 RFC 3339 格式的 `expires_at`                                        |
| PATCH  | `/api/v1/keys/:id`        | 使用 `name` 和 `scopes` 替换名称及权限；不会返回或替换 token                                          |
| POST   | `/api/v1/keys/:id/rotate` | 轮换凭据；scope 和过期时间保持不变                                                                    |
| DELETE | `/api/v1/keys/:id`        | 吊销                                                                                                  |
| GET    | `/api/v1/key-scopes`      | 返回当前身份可选的 `projects`、`permissions`、`global_permissions`（仅限全局的权限）和 `all_projects` |

API Key 查询 scope 选项时，`projects` 只包含具有 `projects:read` 的项目；实际授权仍需满足 `keys:write` 和权限子集约束。

创建成功返回 HTTP 201：

```json
{
  "key": {
    "id": 42,
    "name": "billing-worker",
    "prefix": "cc_exampl…",
    "scopes": [{ "project": "billing", "permissions": ["config:read"] }],
    "created_at": "2026-01-01T00:00:00Z",
    "last_used_at": null,
    "expires_at": null
  },
  "token": "cc_REPLACE_WITH_THE_ISSUED_TOKEN"
}
```

列表返回 `{"keys":[...]}`；编辑返回 `{"key":{...}}`；轮换返回 `{"token":"cc_..."}`。完整 token 只在创建和轮换时显示，服务端仅保存摘要，列表和编辑响应不返回 token 或摘要。旧 token 在轮换或吊销后失效。并发修改导致授权检查过期时返回 HTTP 409，需要重新读取后重试。

浏览器普通用户的列表只包含自己创建的 Key；管理员可查看所有 Key。API Key 调用列表需要 `keys:read`，返回结果也会按目标 Key 的项目和权限过滤。删除用户账号会撤销该用户创建的全部 Key。

## 身份、用户与系统设置

这些接口接受 API Key 或浏览器会话。浏览器用户管理和系统设置要求管理员身份，修改请求需要 CSRF Token。

| 方法   | 路径                | 权限 / 请求体                                                  |
| ------ | ------------------- | -------------------------------------------------------------- |
| GET    | `/api/v1/me`        | 已认证；浏览器返回用户信息和 `csrf`，API Key 返回 `key` 元数据 |
| GET    | `/api/v1/users`     | `*` 的 `users:read`                                            |
| PATCH  | `/api/v1/users/:id` | `*` 的 `users:write`；`role` 为 `user` 或 `admin`              |
| DELETE | `/api/v1/users/:id` | `*` 的 `users:delete`                                          |
| GET    | `/api/v1/settings`  | `*` 的 `settings:read`                                         |
| PUT    | `/api/v1/settings`  | `*` 的 `settings:write`；`allow_registration` 必填布尔值       |

不能删除调用者自己的账号，也不能将自己从管理员降级。Key 调用以 Key 的创建者作为操作账号。设置读取响应为 `{"settings":[{"key":"allow_registration","value":false}]}`，保存成功返回 HTTP 204。

以上全部 API 都可通过统一 [MCP 接口](/docs/mcp) 调用，权限和业务行为保持一致。
