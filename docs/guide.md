# ConfigCenter guide

Self-hosted configuration service: projects as isolation boundaries, environments, OAuth single sign-on, API keys with configurable project and operation scopes, a responsive Vue web UI and an MCP endpoint for agents.

## Integration documentation

Open `/docs/` on your running server for the integration documentation. Public Markdown endpoints `/docs/index.md`, `/docs/api.md`, `/docs/sdk.md` and `/docs/mcp.md` support agents without JavaScript or login. Sources: [overview](index.md), [HTTP API](api.md), [Go SDK](sdk.md), [MCP](mcp.md). The guides are embedded in the Go binary. Documentation pages share the application header, design tokens and saved light/dark theme; their content is pre-rendered with Shiki code highlighting at build time and Markdown endpoints remain public. Shiki generates light/dark token styles in an external stylesheet, keeping the CSP intact and adding no browser-side highlighter.

## Stack

- Go 1.26, gin and gorm with SQLite.
- A Go HTML/JSON shell plus Vue 3, built with Vite Plus.
- shadcn/ui-style Tailwind CSS with Remix Icon, using a vendored font without inline SVG.
- Transient messages as [ding](https://github.com/YusufCeng1z/ding) toasts.
- An OAuth 2.0 authorization-code + PKCE client for OIDC issuers or plain OAuth servers with a userinfo endpoint.

## Quick start

```sh
make bootstrap
make build          # bundles the web assets, then builds .tmp/bin/configcenter
CC_DB=file:.tmp/configcenter.db \
CC_OAUTH_ISSUER=https://idp.example.com \
CC_OAUTH_CLIENT_ID=configcenter \
CC_OAUTH_CLIENT_SECRET=... \
.tmp/bin/configcenter
```

Open http://127.0.0.1:8080. Without OAuth configured the server still starts; only the login link is disabled.

GitHub can be used as the identity provider with `CC_OAUTH_ISSUER=https://github.com` and an OAuth app with callback `CC_PUBLIC_URL/oauth/callback`. GitHub publishes no OIDC discovery document, so the client:

- Falls back to GitHub's documented OAuth endpoints.
- Sends the client ID and secret in the token request form.
- Skips `openid` and PKCE.
- Reads the profile from `/user` and the primary address from `/user/emails`.

Accounts without a public email get `<id>@github.com`.

## Configuration

The binary uses its own [Go SDK](sdk.md) to load configuration once at startup in `internal/config`. It reads the optional `.env.local` through the SDK's dotenv source without changing process variables; `CC_ENV_FILE` selects another file, and a missing file is allowed.

Runtime settings resolve in this order:

1. ConfigCenter values from the selected project and environment.
2. Exported process environment variables.
3. The local env file.
4. Application defaults.

Remote empty strings are explicit values: clearing an OAuth setting remotely does not restore its old local value. Empty local settings retain their default behavior. `CC_OAUTH_ISSUER` and `CC_PUBLIC_URL` are trimmed of trailing slashes; the callback defaults to `CC_PUBLIC_URL/oauth/callback`.

The SDK connection is bootstrapped only from process variables or the local env file; exported variables take precedence. Remote settings cannot redirect this connection.

| Bootstrap variable | Default | Meaning |
| --- | --- | --- |
| `CC_ENV_FILE` | `.env.local` | Optional local bootstrap and fallback settings |
| `CC_CONFIGCENTER_URL` | | ConfigCenter server root; omit together with the API key for local startup |
| `CC_CONFIGCENTER_API_KEY` | | API key scoped to the project holding this server's settings |
| `CC_CONFIGCENTER_ENVIRONMENT` | `production` | Environment to read within that project |

The SDK requests the `CC_` prefix through the secret-reveal endpoint, so the key needs `projects:read`, `environments:read`, `config:read` and `config:read-secrets` on exactly one project. Store session secrets, database credentials and OAuth client secrets as secret configuration items.

Remote reads time out after five seconds. A failed read logs a warning and uses local settings, allowing a single instance to restart when its own API is unavailable. Keep the required database and authentication fallback settings locally for this case.

Invalid bootstrap settings, malformed dotenv files, invalid resolved configuration and process cancellation fail startup. ConfigCenter does not write fallback values to the remote project or reload settings automatically; restart to apply changes. Startup logs include the listen address, public URL and OAuth enabled state, never secret values.

To use AuthCenter, store `CC_OAUTH_ISSUER=https://auth.xeu.life`, its registered client ID and secret, the required scopes and callback in the selected project environment. These settings then override older local GitHub configuration.

| Variable                                        | Default                       | Meaning                                                                  |
| ----------------------------------------------- | ----------------------------- | ------------------------------------------------------------------------ |
| `CC_ADDR`                                       | `127.0.0.1:8080`              | Listen address                                                           |
| `CC_DB_DRIVER`                                  | `sqlite`                      | Database backend: `sqlite` or `mysql`                                    |
| `CC_DB`                                         | `file:configcenter.db`        | SQLite or MySQL DSN                                                      |
| `CC_PUBLIC_URL`                                 | `http://127.0.0.1:8080`       | Public base URL                                                          |
| `CC_OAUTH_ISSUER`                               |                               | OIDC issuer (discovery at `/.well-known/openid-configuration`)           |
| `CC_OAUTH_CLIENT_ID` / `CC_OAUTH_CLIENT_SECRET` |                               | OAuth client; redirect URI is `CC_PUBLIC_URL/oauth/callback`             |
| `CC_OAUTH_SCOPES`                               | `openid profile email groups` | Requested scopes                                                         |
| `CC_OAUTH_REDIRECT_URL`                         | derived                       | Override the callback URL                                                |
| `CC_OAUTH_GROUPS_CLAIM`                         | `groups`                      | Claim used for group mapping                                             |
| `CC_OAUTH_ADMIN_GROUPS`                         | `configcenter-admin`          | Groups that grant the admin role                                         |
| `CC_OAUTH_GROUP_MAP`                            |                               | `group:role,…` for user/admin defaults                                   |
| `CC_SESSION_SECRET`                             |                               | Required when `CC_ENV=production`                                        |
| `CC_LANGUAGES`                                  | `en,zh-CN`                    | Enabled UI languages; tags must match `internal/i18n/locales/<tag>.json` |
| `CC_BOOTSTRAP_ADMIN_EMAIL` / `_NAME`            | `admin@localhost` / `admin`   | First administrator, created when the user table is empty                |

First login creates the user: linked by OAuth subject, adopted by verified email, otherwise new. Roles are managed afterwards in **Users** (admin only); group membership decides the role only when an account is created.

The header theme button switches between light and dark modes. Before a manual choice, the UI follows the system setting; the choice is saved in browser storage and shared across tabs.

## Languages

The UI ships in English and Simplified Chinese (`internal/i18n/locales`). The header dropdown lists `CC_LANGUAGES`, `/language?lang=<tag>` stores the choice in the `cc_lang` cookie and redirects back to the page; without a choice the server negotiates `Accept-Language`. Go supplies translated messages in the page JSON. Vue uses `web/ui/src/i18n.ts` and the same locale catalogs for dialogs, role badges and errors. Add a language by dropping `<tag>.json` into the locales directory and listing the tag in `CC_LANGUAGES`.

## Isolation model

- A **project** is the boundary for configuration, environments and proposals.
- Each **API key** has explicit `scopes`: project slugs (or `*`) paired with permissions such as `config:read`, `config:write` and `config:delete`. A key can read one project and write another. Permissions are checked independently by REST and MCP.
- Users manage their own keys at `/keys` and can grant access to projects they own. Administrators can manage all keys and grant wildcard scopes. Key delegation requires `keys:write` and cannot exceed the caller's scope.
- Editing scopes keeps the token and changes authorization on subsequent requests. Global `users:read/write/delete` and `settings:read/write` permissions support user administration and system settings.

## REST API

Project and key management accept a browser session with `X-CSRF-Token` on mutations, or an API key with the required scopes. User administration and settings require an administrator session. See [HTTP API](api.md) for routes, request bodies and key lifecycle details.

| Method         | Path                                                                   |
| -------------- | ---------------------------------------------------------------------- |
| GET            | `/api/v1/me`                                                           |
| GET/POST       | `/api/v1/keys`, PATCH/DELETE `/api/v1/keys/:id`, POST `…/:id/rotate`   |
| GET/POST       | `/api/v1/projects`                                                     |
| PATCH/DELETE   | `/api/v1/projects/:project`                                            |
| GET/POST       | `/api/v1/projects/:project/environments`                               |
| DELETE         | `/api/v1/projects/:project/environments/:env`                          |
| GET            | `/api/v1/projects/:project/environments/:env/items?prefix=`            |
| GET/PUT/DELETE | `/api/v1/projects/:project/environments/:env/items/:key`               |
| GET/POST       | `/api/v1/projects/:project/proposals?status=pending`                   |
| POST           | `/api/v1/projects/:project/proposals/:id/review` (`{"approve": true}`) |
| GET            | `/api/v1/agent/project` (API key)                                      |
| GET            | `/api/v1/agent/environments/:env/values?prefix=` (API key, read-only)  |
| GET            | `/api/v1/users`, PATCH/DELETE `/api/v1/users/:id`                      |
| GET/PUT        | `/api/v1/settings`                                                     |
| GET            | `/api/v1/key-scopes`                                                   |

## MCP

See the [MCP guide](mcp.md) for authentication, initialization, all 28 tools, examples and errors.

`POST /mcp` speaks JSON-RPC 2.0 over stateless HTTP POST with JSON responses. Authenticate with `Authorization: Bearer <token>`; project operations take an explicit `project` argument. API keys may manage multiple projects with independent scopes. Browser sessions can call management tools with their CSRF token and account permissions. The former `ApiKey` authorization scheme is no longer accepted.

MCP also supports OAuth login through the configured identity provider, with discovery, dynamic public client registration, explicit user consent, S256 PKCE, rotating refresh tokens and revocation. Configure the existing `CC_OAUTH_*` settings and a public HTTPS `CC_PUBLIC_URL`, then select OAuth in the MCP client. OAuth tokens are restricted to `/mcp` and follow the account’s current permissions.

MCP exposes every API operation: project and environment management, configuration reads and writes, proposals and reviews, key lifecycle, user administration, settings and identity. REST and MCP share operation definitions and handlers. Discovery filters tools by granted scopes, and execution validates permissions for the actual target. User and settings scopes require the `*` project range. Results include JSON text and `structuredContent` with the API response and `http_status`.

## Go SDK

`sdk/configcenter` is a separate module (`github.com/OXeu/configcenter/sdk/configcenter`) with a TTL-based memory cache and godotenv support:

```go
client, err := configcenter.New(configcenter.Config{
	BaseURL: os.Getenv("CC_URL"), APIKey: os.Getenv("CC_KEY"),
})
if err != nil { /* handle */ }
if err := client.Load(ctx); err != nil { /* handle */ }
pool := client.Int("db.pool", 10)
if err := client.Watch(ctx, configcenter.WatchOptions{Interval: 30 * time.Second}); err != nil { /* handle */ }
```

The key decides the project, so the SDK never takes a project name.

Reads resolve each key in this order:

1. Unexpired memory cache.
2. ConfigCenter.
3. godotenv files.
4. `Config.Defaults` or the getter's default.

Empty strings count as configured values. Each client has an isolated memory map; `CacheTTL` defaults to 30 seconds and cache hits never extend it. `Load` and `Watch` skip remote requests when their full scope is cached, and refresh after expiry. Getters reject expired entries and use loaded fallbacks without network requests. No cache is persisted to disk.

`DotenvFiles` defaults to an optional `.env` without changing process environment variables. Source failures return errors while exposing available fallback values. When both remote connection fields are empty, `Put` writes only to memory with the same TTL; partial remote configuration is rejected. See the [SDK guide](sdk.md) for initialization, source composition and error handling.

## Development

Use Node 22.18+ or 24.11+ (supported Vite Plus LTS versions), npm and Go 1.26.10, with golangci-lint installed.

```sh
make bootstrap
make check   # build, Go lint/tests, Oxlint, Vue type checking, Vitest, SDK lint/tests
make build   # embed .tmp/ui and produce .tmp/bin/configcenter
make run     # build and start the Go server
npm run dev # in a second terminal; open http://localhost:5173/projects
```

The Vite development server gets authenticated page JSON from Go at `127.0.0.1:8080` and serves Vue with hot reload. API, OAuth, language and logout requests are proxied to Go. Set `CC_PUBLIC_URL=http://localhost:5173` and register that origin's `/oauth/callback` with your provider when testing OAuth through Vite. Production serves embedded JavaScript and CSS directly from Go; Node is only needed at build time.

Vite Plus provides the build, Oxlint, Oxfmt and Vitest. `vue-tsc` checks both scripts and templates. Oxlint custom plugins inspect script ASTs; separate compiler-based Vitest checks enforce template constraints. See [AGENTS.md](AGENTS.md).

Build output stays in ignored `.tmp/ui` and `.tmp/docs` and is embedded by `assets.go`. `npm run build:docs` renders Markdown and Shiki HTML/CSS; `make assets` runs it before the Vite build. Run `make assets` before direct Go commands on a fresh checkout; Make targets do this automatically. Go scratch files use `.tmp`, and binaries use `.tmp/bin`. The live SDK test is opt-in through `CC_TEST_URL` and `CC_TEST_KEY`.

Type checking uses TypeScript 7 (`typescript-native`) for modules, tools and non-component tests. Vue template and component tests use `vue-tsc` with TypeScript 6 because Volar requires its JavaScript compiler API. Run both with `npm run typecheck`.

### MySQL

Local development defaults to SQLite. To connect to MySQL, set:

```dotenv
CC_DB_DRIVER=mysql
CC_DB=user:password@tcp(mysql-host:3306)/configcenter?charset=utf8mb4&tls=true
```

Create the database first (MySQL 8+, utf8mb4 with a case-sensitive collation such
as utf8mb4_bin). The application creates tables and enables timestamp parsing.
Keep credentials in environment variables or the ignored .env.local file.

### Tencent Cloud Functions

The GitHub workflow runs `make check`, MySQL integration tests and `make build`.
Deployments from main are enabled by repository variable `SCF_DEPLOY_ENABLED=true`;
leave it unset until production configuration is ready.

Configure variables `SCF_REGION`, `SCF_VPC_ID`, `SCF_SUBNET_ID` for the network
that can reach MySQL. Configure secrets `TENCENT_SECRET_ID`,
`TENCENT_SECRET_KEY`, and `SCF_RUNTIME_ENV`. The last secret is a dotenv file:

```dotenv
CC_DB=user:password@tcp(mysql-host:3306)/configcenter?charset=utf8mb4
CC_SESSION_SECRET=replace-with-a-long-random-secret
CC_PUBLIC_URL=https://your-domain.example
CC_OAUTH_ISSUER=https://your-identity-provider.example
CC_OAUTH_CLIENT_ID=your-client-id
CC_OAUTH_CLIENT_SECRET=your-client-secret
CC_BOOTSTRAP_ADMIN_EMAIL=admin@example.com
```

The workflow packages this file privately with the function code, removes the
runner copy after deployment, and never uploads the package as a GitHub artifact.
The selected Tencent action runs serverless-cloud-framework; its source is pinned,
but its upstream CLI installation is not version-pinned.

SCF forces MySQL, production mode and port 9000. The HTTP trigger exposes a
function URL with public and private access. Cloud-level authentication is NONE;
the application enforces OAuth sessions and API keys.

1. Set `CC_PUBLIC_URL` to the function URL or its custom domain.
2. Register its `/oauth/callback` URL at the identity provider.
3. Permit outbound access to the identity provider from the VPC.

SCF uses Beijing time (`TZ=Asia/Shanghai`), 64 MB of memory, static concurrency
of 100 requests per instance and a one-second request buffer. After deploying,
the workflow runs `tools/scf-network.py configcenter` with the SCF Python SDK to
enable and verify fixed intranet egress IPs; the SCF component does not forward
the `IntranetConfig` API setting. Manual deployments must run this step too.

Changing the driver does not migrate existing SQLite data to MySQL.

## 共享基础设施

本项目通过 `common.lock.json` 固定 center-common 的提交，公共源码与 CI 步骤均来自同一个版本。首次检出先执行 `make bootstrap`，再运行 `make check build`。私有依赖使用本机 Git 凭据；CI 配置只读 `COMMON_REPO_SSH_KEY` deploy key（或 `COMMON_REPO_TOKEN`）secret。

本地同时开发四个独立仓库时，执行 `make common-local COMMON_DIR=../center-common && make bootstrap`。三个项目的 `.tmp/common` 会直接引用同一份公共源码，修改无需复制。共享 Vue 控件在开发服务器热更新，Go 修改需重新构建。可复现部署使用 `CI=true make bootstrap check build`；该模式忽略本地覆盖并拉取锁定提交。

升级时先提交并发布 common，再更新本项目锁文件和必要的适配；回滚恢复原锁文件与适配。公共能力、工具链和维护流程见 [center-common README](https://github.com/OXeu/center-common#readme)。业务页面、表单、权限和部署凭据仍在本仓库维护。
