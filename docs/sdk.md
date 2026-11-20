# Go SDK 接入

[文档首页](/docs/) · [HTTP API](/docs/api) · [MCP 接口](/docs/mcp) · [Markdown 原文](/docs/sdk.md)

SDK 使用只授权一个具体项目的 API Key，普通配置读取需要 `config:read`，密钥返回掩码；调用项目环境发现方法还需要 `projects:read` 和 `environments:read`。多项目或 `*` scope 请使用 [HTTP API](/docs/api) 或 [MCP](/docs/mcp)，SDK 不提供选择项目的参数。

需要在应用执行路径使用密钥明文时，显式设置 `Config.RevealSecrets: true`，并为该应用的 API Key 授予 `config:read-secrets`。此选项使 `Load`、`LoadPrefix`、`ReadMany` 和轮询读取使用专用的 `/values/reveal` 接口，不会降级重试普通读取。权限不足时，快照加载通过错误回调或告警报告并使用本地来源；直接调用 `ReadMany` 仍返回错误。默认保持掩码读取，`Item.Secret` 与 `Item.Masked` 可用于区分密钥及其返回形式。只启用特权读取时无需额外授予 `config:read`，写入及项目发现仍需要各自的 scope。

## 安装

SDK 是独立 Go 模块，需要 Go 1.26 或更高版本。缓存使用带 TTL 的内存 map，dotenv 文件使用 godotenv 解析，不依赖本地 KV 数据库。在调用方 Go 模块目录执行：

```sh
go get github.com/OXeu/configcenter/sdk/configcenter
```

生产项目在 go.mod 中固定并审查实际解析的版本。若使用尚未发布的本地修改，可在调用方模块中增加 `replace github.com/OXeu/configcenter/sdk/configcenter => /absolute/path/to/configcenter/sdk/configcenter`。

## 初始化和首次读取

以下函数可放入调用方应用，将地址和密钥从调用方配置层传入：

```go
package integration

import (
    "context"
    "fmt"
    "time"

    "github.com/OXeu/configcenter/sdk/configcenter"
)

func LoadPool(ctx context.Context, baseURL, apiKey string) (int, error) {
    client, err := configcenter.New(configcenter.Config{
        BaseURL: baseURL,
        APIKey: apiKey,
        Environment: "production",
        Timeout: 5 * time.Second,
        Defaults: map[string]string{"db.pool": "10"},
    })
    if err != nil {
        return 0, fmt.Errorf("create configuration client: %w", err)
    }
    if err := client.Load(ctx); err != nil {
        return 0, fmt.Errorf("load configuration: %w", err)
    }
    return client.Int("db.pool", 10), nil
}
```

`BaseURL` 是服务根地址，不包含 `/api/v1/agent`。`New` 仅校验本地配置，不发请求，也不验证密钥是否有效。`Environment` 为空时使用 `production`。默认请求超时为 30 秒；提供 `HTTPClient` 时使用该客户端，`Timeout` 字段不再生效。

启动配置读取使用 `Load` 或 `LoadPrefix`。远程服务不可达、超时、HTTP 错误（包括非标准的 443 状态）或无效响应不会使这些方法返回错误，而是通过 `Config.OnError` 报告；未设置回调时使用 `slog` 输出告警。应用可从已加载的回退来源或调用方默认值继续启动，仍需自行校验必填配置。

## 多级 fallback 与远程缓存

SDK 按键合并数据源，优先级依次为：

1. 尚未过期的内存缓存。
2. ConfigCenter 当前返回的配置。
3. godotenv 解析的 dotenv 文件。
4. `Config.Defaults`；仍不存在时使用 `GetDefault`、`Int`、`Bool` 的调用方默认值。

空字符串是已配置的值，不会继续向下查找。`Int`、`Bool` 对选中的值解析失败时直接返回调用方默认值，不再尝试较低优先级的数据源。

```go
client, err := configcenter.New(configcenter.Config{
    BaseURL: baseURL,
    APIKey: apiKey,
    Environment: "production",
    CacheTTL: 30 * time.Second,
    DotenvFiles: []string{".env.local", ".env"},
    Defaults: map[string]string{"db.pool": "10"},
    OnError: func(err error) {
        logger.Warn("remote configuration unavailable; using local values", "error", err)
    },
})
if err != nil { return fmt.Errorf("create configuration client: %w", err) }
if err := client.Load(ctx); err != nil {
    return fmt.Errorf("load local configuration: %w", err)
}
pool := client.Int("db.pool", 10)
```

此片段放在持有 `ctx`、`baseURL`、`apiKey` 和 `logger` 的调用方函数中。远程读取失败时，`Load` 合并可用来源、发布快照，再在释放内部锁后同步调用错误回调；回调可读取客户端。显式 dotenv 文件缺失、解析失败及调用方 context 取消仍返回错误。过期缓存不会用于降级；没有可用来源时快照为空，可使用 `GetDefault`、`Int`、`Bool` 的默认值。context 取消时保留原加载范围和 fallback，但缓存仍按原 TTL 到期。

`BaseURL` 和 `APIKey` 同时为空时跳过远端；仅填其中一个仍报错。远程请求失败不会将写操作转存到本地。

### 内存缓存与 TTL

`CacheTTL` 为零时使用 `DefaultCacheTTL`（30 秒），也可指定正的 `time.Duration`，负值会在 `New` 时返回错误。TTL 从成功读取或写入开始计算，命中缓存不会续期。每个客户端持有独立的 map，默认不跨客户端或进程重启共享，也不需要 Close；需要跨重启复用时可设置 `CacheFile`。

`Load` / `LoadPrefix` 先检查请求范围是否有完整且有效的缓存；命中时跳过 ConfigCenter。缓存缺失或过期时请求远端，用返回值补齐缓存；已有的未过期值仍优先。完整快照可以覆盖更窄的前缀，前缀快照不能代替完整加载。成功返回的空快照也按 TTL 缓存，避免反复请求缺失的配置。更大范围的加载不会延长已有条目的 TTL。

远程读取和 `Put` 成功后缓存空字符串及配置元数据；失败的写入不改动缓存。过期后远端已删除的键、远端不可用时的旧值均不会继续返回，而是使用 dotenv 或默认值。加载和写入会清理过期条目；`Get`、`GetDefault`、`Int`、`Bool` 和 `All` 也会检查有效期。

dotenv 和 `Defaults` 仅作为读取 fallback，不会写入远程缓存。读取方法不发网络请求：缓存过期时使用最近一次加载的 fallback，需要调用 `Load` 或启用 `Watch` 获取最新远程值。原有 `LocalPath`、`LocalDefaults` 和 `DefaultLocalPath` 已移除；应用默认值改用 `Defaults`，持久化配置写入 ConfigCenter。

### 本地文件缓存

`CacheFile` 指定一个本地快照文件，默认关闭。`New` 会同步读取该文件，使进程在首次网络请求完成前就能返回上次成功加载的远程配置；文件按环境区分，环境或版本不匹配时忽略。文件只在成功加载后由 SDK 原子替换，SDK 不会删除它。

快照同时记录服务端返回的校验值和加载时间。文件仍在 `CacheTTL` 内时，`Load` 不发请求；超过 TTL 后条目对读取方法不可见，但 SDK 仍保留校验值，下一次 `Load` 或轮询使用条件请求确认，未变化则不发正文并恢复这些值。这样重启不会造成批量全量下载。

```go
client, err := configcenter.New(configcenter.Config{
    BaseURL:   baseURL,
    APIKey:    apiKey,
    CacheFile: ".tmp/configcenter-cache.json",
    CacheTTL:  30 * time.Second,
})
```

此片段放在持有 `baseURL` 和 `apiKey` 的调用方函数中。相对路径按进程工作目录解析，父目录会自动创建；无法读取或内容损坏时会忽略文件并通过 `slog` 告警，不会阻止启动。文件包含掩码或明文配置值，应与 `.env` 同等保护：放在仓库 `.tmp/` 或应用专用的受限目录，不要提交到版本库，也不要跨环境共享。

### dotenv 文件

`DotenvFiles` 为 `nil` 时读取当前目录下可选的 `.env`，文件不存在不报错。显式空切片 `[]string{}` 禁用 dotenv；显式文件列表中，前面的文件优先，缺失或无效文件会返回错误，其余来源仍可使用。

每次 `Load` / `LoadPrefix` / Watch 刷新都会重新读取文件。godotenv 支持引号、`export` 和文件内变量展开；键名保持原样，不自动将 `db.pool` 转成 `DB_POOL`。SDK 使用 `godotenv.Read`，不导入或修改进程环境变量；应用需要进程变量时由自己的配置层处理。解析错误不会包含 dotenv 文件内容。

### 写入和默认值初始化

`Put(ctx, key, value)` 在有远端配置时写入远端，成功后更新内存缓存；未配置远端时仅写入当前客户端的内存，同样受 TTL 限制。匹配当前加载前缀的写入可立即读取，其他前缀需重新加载对应范围。远端写入需要 `projects:read`、`environments:read` 和 `config:write`。`IsLocal()` 表示是否未配置远端。配置键遵循服务端的 1–191 字符规则。

`ReadMany(ctx, keys)` 一次请求读取指定键，返回 `map[string]string`，缺失键不在结果中。它直接读取后端，绕过远端缓存和 fallback，不替换加载快照；失败仍返回错误，启动时需要回退的读取应使用 `Load` 后调用 `Get`。

`PutMany(ctx, values)` 在一个事务中创建或更新所有指定键，并更新缓存；需要的远端权限与 `Put` 相同。每批 1–500 个键，写入请求体最多 1 MiB。

`EnsureDefaults(ctx, defaults)` 批量读取后端，再通过 `PutMany` 写入缺失项，最后刷新快照；已读取到的值保持不变。它使用现有 `config:read`、`config:write` 权限，SDK 项目定位还需 `projects:read`、`environments:read`。远端首次初始化应由一个实例完成，先读后写不是跨请求事务。低优先级来源已有值不会阻止初始化远端缺失项。未配置远端时只初始化当前客户端中缺失或已过期的内存条目，仍受 TTL 限制；长期 fallback 应使用 `Config.Defaults`。

```go
if err := client.PutMany(ctx, map[string]string{"db.pool": "10", "feature.enabled": "false"}); err != nil {
    return err
}
values, err := client.ReadMany(ctx, []string{"db.pool", "feature.enabled"})
```

## 统一数据源抽象

远端、dotenv 和默认值使用同一个读取接口，客户端在远端请求前检查内存缓存：

```go
type DataSource interface {
    Read(ctx context.Context, prefix string) ([]Item, error)
}
```

公开的 `Sources` 可组合任意实现该接口的数据源，按顺序为每个键选取第一个值，并聚合各来源错误。来源可以同时返回可用数据和错误。`MapSource` 提供静态内存配置（自身不带 TTL），`DotenvSource` 提供文件配置；SDK 内部使用它们组合 fallback，TTL 由客户端缓存管理。

```go
var source configcenter.DataSource = configcenter.Sources{
    configcenter.DotenvSource{Files: []string{".env"}},
    configcenter.MapSource{"db.pool": "10"},
}
items, err := source.Read(ctx, "db.")
```

`Read` 返回匹配前缀的配置项，不修改客户端快照；`Sources.Read` 按键排序。调用期间不要修改传入的 map 或文件列表。`New` 会复制 `Config` 中的默认值 map 和文件列表。

### 共享配置快照

`LoadSnapshot(ctx, prefix, sources...)` 是 SDK 与 ConfigCenter 内部共用的配置读取层，统一处理来源优先级、前缀过滤、空值和错误传播。返回的 `Snapshot` 提供 `Get`、`GetDefault` 和按键排序的 `All`，其内容不会随来源或返回切片的修改而变化。

```go
snapshot, err := configcenter.LoadSnapshot(ctx, "db.",
    configcenter.DotenvSource{Files: []string{".env.local"}},
    configcenter.MapSource{"db.pool": "10"},
)
if err != nil {
    return fmt.Errorf("load settings: %w", err)
}
pool := snapshot.GetDefault("db.pool", "10")
```

SDK 的 HTTP 读取、dotenv 和默认值均通过这层加载；原有内存缓存和 TTL 行为保持不变。服务端的 `Store.ConfigurationSource` 实现同一个 `DataSource` 接口，直接查询已有数据库连接，不需要 HTTP 或 API Key。独立 SDK 不依赖数据库驱动或服务端包。

ConfigCenter 启动时从本地配置打开数据库，再读取 `configcenter/production` 中的 `CC_*` 设置，并把同一连接交给 HTTP 服务。数据库连接和首次管理员设置只由本地提供，避免启动循环；缺失运行时键回退本地，数据库读取失败则终止启动。

## 快照读取

| 方法                        | 行为                                               |
| --------------------------- | -------------------------------------------------- |
| `Load(ctx)`                 | 合并来源；远程失败告警并使用回退，本地错误和取消返回错误 |
| `LoadPrefix(ctx, "db.")`    | 逐层合并匹配前缀的配置，替换整个快照               |
| `Get(key)`                  | 返回未过期缓存或已加载的 fallback，不发网络请求    |
| `GetDefault(key, fallback)` | 缺失时返回默认值；空字符串仍是有效值               |
| `Int(key, fallback)`        | 去除两端空白后解析整数，缺失或解析失败时返回默认值 |
| `Bool(key, fallback)`       | 使用 Go `strconv.ParseBool`，失败时返回默认值      |
| `All()`                     | 返回按 key 排序的配置项副本                        |
| `Environment()`             | 返回当前客户端环境 slug                            |

新建客户端的快照为空；首次 `Load` 加载 fallback，成功的 `Put` 也可立即提供缓存值。读取和快照替换受锁保护，可并发调用。来源失败时可使用其他来源的值，过期缓存始终不可见；context 取消时保留原加载范围和 fallback。需要区分缺失和无效值时，使用 `Get` 后自行解析。配置中的 JSON 也需要调用方自行反序列化。

## 自动刷新

```go
watchCtx, cancel := context.WithCancel(ctx)
defer cancel()
if err := client.Watch(watchCtx, configcenter.WatchOptions{
    Interval: 30 * time.Second,
    OnError: func(err error) {
        logger.Error("refresh configuration", "error", err)
    },
}); err != nil {
    return fmt.Errorf("start configuration watcher: %w", err)
}
```

此片段放在持有 `client`、`ctx` 和 `logger` 的长生命周期函数中，并在该函数结束前运行应用。Watch 同步加载首次完整快照；远程失败时使用回退、报告错误并继续启动后台轮询，本地错误或 context 取消则返回错误。Interval 小于等于零时使用 30 秒。首次及后续远程错误优先交给 `WatchOptions.OnError`，否则使用 `Config.OnError`，两者均未设置时输出告警。远端失败不会缓存为空结果，恢复后下一次轮询可立即重新读取。

取消 context 可停止轮询。一个客户端通常只启动一次 Watch。Watch 每次加载全部配置，因此会覆盖 LoadPrefix 得到的过滤快照；它不提供前缀订阅或服务端推送。

轮询遵守 `CacheTTL`：缓存有效时跳过远端，过期后的下一次轮询才用条件请求确认校验值。服务端对普通读取返回基于响应正文的 `ETag`，未变化时回复 `304 Not Modified`，不会重复传输配置正文；客户端收到 `304` 只延长 TTL，不替换快照。`OnChange` 只在配置内容真正变化时调用，键序、时间戳和 TTL 变化都不会触发。

```go
if err := client.Watch(watchCtx, configcenter.WatchOptions{
    Interval: 30 * time.Second,
    OnChange: func(items []configcenter.Item) {
        logger.Info("configuration changed", "keys", len(items))
    },
}); err != nil {
    return fmt.Errorf("start configuration watcher: %w", err)
}
```

调用方可以据此重建连接池、刷新证书或重新加载业务规则，不必在每次轮询后与自身保存的副本比较内容。服务端未返回 `ETag` 时，SDK 用收到的正文计算同样的校验值，仍能避免重复发布。本地写入会清除校验值，使下一次读取重新获取完整正文。

## 错误处理

使用 `errors.Is(err, configcenter.ErrNotFound)` 判断 HTTP 404（例如环境不存在）；快照加载的错误来自回调，直接后端操作的错误来自返回值。其他非 200 状态包含状态码，错误信息不包含响应正文或 API Key。`Get` 通过 bool 表示缺失，并不返回 `ErrKeyNotFound`。

`Load`、`LoadPrefix` 和 `Watch` 的远程故障容错不会改变共享 `LoadSnapshot` / `Sources.Read` 的错误传播行为。`ReadMany`、`Put`、`PutMany`、`EnsureDefaults` 仍对后端错误返回失败，包括初始化后的强制刷新失败；不会因远程不可用而把写入转存本地。
