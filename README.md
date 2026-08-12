# DomainHunter

<p align="center">
  <img src="web/public/DomainHunter.svg" alt="DomainHunter" width="180" />
</p>

DomainHunter 是一个面向**长期监控**的 Go 域名状态查询器。查询链路建立在
"RDAP 优先、WHOIS 兼容、**无法确认就不报告可注册**"的安全模型上，
并为每个结论保留可追溯的查询证据。

单体 Go 应用 + SQLite + 单容器，适合树莓派、Synology NAS 与普通 Linux VPS 自托管。

| 文档 | 内容 |
| --- | --- |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 目录结构、运行时数据流、查询引擎的决策顺序、调度与存储设计 |
| [MIGRATION.md](MIGRATION.md) | 从 Puff / v1 升级、数据库文件名、密码迁移、升级步骤与回滚 |
| [HANDOFF.md](HANDOFF.md) | 重构交接：改了什么、验证到什么程度、已知问题、踩坑记录 |

## 设计目标

- 使用 IANA RDAP bootstrap 动态补充 TLD → RDAP 映射，内置静态配置作为离线兜底；
  IANA 不可达时服务照常启动。
- 只有收到**明确的未注册信号**才显示"可注册"。HTTP 404、空 RDAP 对象、超时、
  连接失败、注册局策略拒绝和保留域名都不会被推断为可注册。
- 没有可用查询源的后缀显示"已跳过"，不会把"无法查询"伪装成"可注册"。
- 每次查询都会记录**每个查询源分别看到了什么**（状态、耗时、错误），
  详情页据此回答"当前状态为什么是这个结果"。
- 状态流转有历史：`registered → grace → redemption → pending_delete → available`
  全程可回溯。
- 旧版遗留的 `data/puff.db` 与 `PUFF_*` 环境变量仍被接受，升级无需重建任何数据。

## 快速开始

需要 Go 1.24+ 或 Docker。

```bash
git clone https://github.com/NextCandy/DomainHunter.git
cd DomainHunter
docker compose up -d --build
curl -f http://127.0.0.1:8080/health
```

默认端口 `8080`，默认账号 `domainhunter / domainhunter123`，**登录后请立即修改密码**。

升级已有安装只需把原数据目录挂到 `/app/data`，账号、域名列表与监控设置
会继续使用数据库中的值。旧文件名 `puff.db` 也能被识别，详见
[MIGRATION.md](MIGRATION.md)。

本地编译运行：

```bash
go test ./...
go build -o domainhunter ./cmd/domainhunter
./domainhunter
```

## 架构

```
HTTP  →  Service  →  Scheduler  →  Worker Pool  →  Query Engine  →  Provider
                          ↑                              ↓
                     Repository  ←──────────────────  SQLite
                                                          ↓
                                                   Notification
```

完整说明见 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 管理界面

React + TypeScript + Vite + Tailwind，构建产物经 `go:embed` 打进同一个二进制，
**生产运行时不需要 Node，仍然只有一个容器**。支持浅色 / 深色 / 跟随系统，
窄屏下表格自动换成卡片列表。

| 页面 | 作用 |
| --- | --- |
| 概览 | 状态统计、最近状态变化、即将到期、最近可注册、查询失败、查询源健康 |
| 域名 | 全量列表：搜索、状态 / 后缀 / 注册商 / 查询源筛选、排序、分页、批量检查与删除、★ 只看收藏 |
| 抢注看板 | **只**显示处于掉落流程的域名（可注册 / 待删除 / 赎回期 / 已过期 / 宽限期），按抢注紧迫度排序并给出距今天数 |
| 查询历史 | 全局状态变化 + 按域名查看完整状态时间线与各查询源历史 |
| 查询源 | 各 Provider 健康度、IANA bootstrap 状态、查询策略编辑 |
| 通知 | 五个渠道的配置与单独测试、通知历史 |
| 系统设置 | 监控参数、历史保留策略、账户、备份与维护 |

域名详情是右侧抽屉，分四个标签页：概览 / 查询证据 / 状态时间线 / 原始报文。
「查询证据」会列出本次结论里**每个查询源分别看到了什么**（状态、耗时、错误），
这是回答"当前状态为什么是这个结果"的地方。

## Docker Compose

仓库内的 `compose.yaml` 使用本地构建镜像，数据只写入 `./data`：

```bash
docker compose config -q
docker compose up -d --build
curl http://127.0.0.1:8080/health
```

备用查询服务是**可选项**，只对已经确认需要它的后缀配置：

```yaml
environment:
  DOMAINHUNTER_WHOIS_FALLBACK_URL: http://host.docker.internal:12121/api/
  DOMAINHUNTER_WHOIS_FALLBACK_TLDS: im,do
  DOMAINHUNTER_WHOIS_FALLBACK_TIMEOUT: "20"
  DOMAINHUNTER_WHOIS_LS_URL: https://whois.ls/json/
  DOMAINHUNTER_WHOIS_LS_TLDS: im
  DOMAINHUNTER_WHOIS_LS_TIMEOUT: "20s"
extra_hosts:
  - host.docker.internal:host-gateway
```

树莓派上的实际部署配置见 `deploy/raspberry-pi.compose.yaml`。

## 数据目录

```
data/
├── domainhunter.db         主数据库
├── puff.db                 旧版遗留文件名；存在时**优先使用**，不会被自动改名
└── backups/
    └── *-YYYYMMDD-HHMMSS-*.db   迁移前自动生成的备份，只保留最近 5 份
```

选择哪个文件的顺序是：`DOMAINHUNTER_DB_FILE` 环境变量 → 已存在的 `puff.db`
→ 已存在的 `domainhunter.db` → 全新安装创建 `domainhunter.db`。
旧部署因此不会被动过，也不会在旁边多出一个空库；想改名的话停容器手动
`mv` 即可，见 [MIGRATION.md](MIGRATION.md)。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `DOMAINHUNTER_DATA_DIR` | `data` | 数据目录 |
| `DOMAINHUNTER_DB_FILE` | 自动选择 | 指定数据库文件名，覆盖自动选择 |
| `DOMAINHUNTER_PORT` | 数据库中的 `server_port`（8080） | 监听端口 |
| `DOMAINHUNTER_RDAP_BOOTSTRAP_URL` | `https://data.iana.org/rdap/dns.json` | IANA bootstrap |
| `DOMAINHUNTER_WHO_DAT_URL` | 空 | 首选 Pi who-dat 地址，例如 `http://host.docker.internal:59090` |
| `DOMAINHUNTER_WHO_DAT_API_KEY` | 空 | 首选 who-dat 的 Bearer Key，不提交到仓库 |
| `DOMAINHUNTER_VERCEL_WHO_DAT_URL` | `https://rdap.re` | 三选 who-dat 兼容服务 |
| `DOMAINHUNTER_RDAP_ORG_URL` | `https://rdap.org` | 五选 RDAP 转发服务 |
| `DOMAINHUNTER_WHOIS_FALLBACK_URL` | 空 | 本地结构化备用服务地址 |
| `DOMAINHUNTER_WHOIS_FALLBACK_TLDS` | `im,do` | 启用备用服务的后缀 |
| `DOMAINHUNTER_WHOIS_FALLBACK_TIMEOUT` | `20` | 秒 |
| `DOMAINHUNTER_WHOIS_LS_URL` | 空 | WHOIS.LS JSON 网关 |
| `DOMAINHUNTER_WHOIS_LS_TLDS` | `im` | 启用 WHOIS.LS 的后缀 |
| `DOMAINHUNTER_WHOIS_LS_TIMEOUT` | `20s` | 支持 `20` 或 `20s` |
| `DOMAINHUNTER_AI_API_KEY` | 空 | 默认 AI 配置的运行时 Key；不会写入仓库 |
| `DOMAINHUNTER_SECRET_KEY` | 空 | UI 保存多个 AI Key 时使用 AES-GCM 加密 |
| `DOMAINHUNTER_AI_ALLOWED_HOSTS` | `api.deepseek.com,opencode.ai` | AI Base URL 主机 allowlist |
| `DOMAINHUNTER_QUERY_POLICY_FILE` | 空 | 查询策略 JSON 文件（优先于数据库设置） |
| `DOMAINHUNTER_COOKIE_SECURE` | `auto` | `auto` / `true` / `false` |
| `DOMAINHUNTER_COOKIE_SAMESITE` | `lax` | `lax` / `strict` / `none` |
| `DOMAINHUNTER_CORS_ORIGINS` | 空 | 逗号分隔；为空表示仅同源 |
| `DOMAINHUNTER_CSRF_ENABLED` | `true` | 写操作的 CSRF 校验 |
| `DOMAINHUNTER_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `DOMAINHUNTER_LOG_FORMAT` | `text` | 设为 `json` 输出结构化日志 |

对应的 `PUFF_*` 旧变量名仍然有效（已废弃，但不会被删除）。

## 查询 Provider

树莓派上的 who-dat 独立服务使用 `deploy/who-dat-raspberry-pi.compose.yaml`，生产实例
采用高位端口 `59090` 并强制 `AUTH_KEY`；DomainHunter 通过 `host.docker.internal:59090`
访问，不把 Key 暴露在仓库或前端。

| Provider | 类型 | 说明 |
| --- | --- | --- |
| `who_dat` | 首选 | Pi 上的 lissy93/who-dat；生产部署默认端口 59090 |
| `whois_domain_lookup` | 次选 | Pi 上的 whois-domain-lookup 结构化 API |
| `vercel_who_dat` | 三选 | `https://rdap.re` 的 who-dat 兼容 API |
| `rdap` | 通用 | 结构化优先；只有合法的 RDAP 错误对象 + 明确的"不存在"语义才判可注册 |
| `rdap_org` | 五选 | `https://rdap.org/domain/{domain}` |
| `ai_fallback` | 兜底 | 当前默认 AI 配置；仅输出研究性说明，不判定可注册 |
| `whois` | 通用 | 注册局 43 端口；状态识别词表见 `internal/registry/detection_patterns.json` |
| `whois_ls` | 按后缀启用 | WHOIS.LS JSON 网关，部署上用于 `.im` |
| `fallback` | 按后缀启用 | 本地 whois-domain-lookup 结构化服务，部署上用于 `.im` / `.do` |

查询顺序与"可注册"的采信程度可以按 TLD 配置（管理端「查询源」页，或
`app_settings.query_policy`）：

```json
{
  "default": {
    "providers": ["who_dat", "whois_domain_lookup", "vercel_who_dat", "rdap", "rdap_org", "ai_fallback"]
  },
  "rate_limits": { "whois_domain_lookup": "1s" }
}
```

`validate_available` 取 `true`（= `"distrust"`，不单独采信）、`"confirm"`
（需要第二个来源印证）或 `"trust"`。**留空即使用默认五级顺序**：
`who_dat → whois_domain_lookup → vercel_who_dat → rdap → rdap_org → ai_fallback`。

`rate_limits` 按 `provider` 或 `provider:tld` 限制两次查询的最小间隔。
`whois_ls` 与 `fallback` 默认各 `1s`：它们指向公共网关或单实例本地服务，
几十个请求同时打过去会让它们从 2 秒返回退化成 20 秒超时，反而把本来能查到的
域名变成 `unknown`。通用的 RDAP / WHOIS 默认不限速。

### 关于 `.im` 与 `.do`

- `.im`：注册局公开响应**不提供创建日期**。DomainHunter 保留创建日期为空，
  绝不猜测或填充伪造日期；到期日通过 WHOIS.LS 获取。
- `.do`：响应会给每个隐私字段追加 `| Registry Policy`。这只是隐私标记，
  明确的 `registered=true` 优先，不会被误判为保留域名。

## 支持状态

`available` `registered` `grace` `redemption` `pending_delete` `expired`
`transfer_locked` `hold` `unknown` `error` `skipped`

## 历史数据

每次查询都可以留下一条观测（`domain_observations`）和每个查询源的一次尝试
（`query_attempts`）。为了让树莓派 / NAS 长期运行不被撑爆，写入与保留都有上限，
在管理端「系统设置 → 历史数据保留」里调：

| 设置 | 默认 | 作用 |
| --- | --- | --- |
| 保留天数 | 180 | 超过就清理；0 表示不按时间清理 |
| 每域名最多保留 | 200 | 超过就丢最旧的；0 表示不限制 |
| **心跳间隔（小时）** | **6** | 状态**没有变化**时两条观测的最小间隔；0 表示每次查询都记 |
| 原始报文 | 仅状态变化时保存 | 也可选总是保存 / 不保存 |
| 单条报文上限 | 16 KB | 超出截断 |

心跳间隔是最关键的一项：800 个域名按 10 分钟一轮，每次都记就是**每天 11 万行**
几乎完全相同的数据。默认设置下状态变化一条不漏，其余每 6 小时留一个心跳。
清理任务每 6 小时跑一次。

## 通知

内置五个渠道，都在管理端「通知」页配置，可以单独开关与单独发测试：

| 渠道 | 说明 |
| --- | --- |
| 邮件 | 标准 SMTP |
| Telegram | Bot Token + Chat ID |
| Bark | iOS 推送，填完整推送地址（含设备 key），官方或自建服务器均可 |
| 飞书机器人 | 群自定义机器人 webhook，支持「签名校验」密钥 |
| 自定义 Webhook | 事件以 JSON POST 出去，可选 `X-DomainHunter-Signature` 验签 |

抑制规则：

- 首次查询不通知（避免初始化时刷屏）
- 从 `error` 恢复不通知
- 目标状态本身不需要通知（`unknown` / `error` / `skipped`）时不通知
- **可注册结论证据不足时不通知**
- Bark 使用精简正文，仅保留状态摘要，不推送 WHOIS/RDAP 原文，正文约 480 字节以内
- 同一轮里多个域名的变化会合并成一条

再加新渠道只需实现 `notification.Notifier` 接口，并在
`Manager.RegisterAll` / `ApplyConfig` 里各加一行。

## API

接口都需要登录（Cookie 会话），写操作要过 CSRF 校验。**旧版接口全部保留，
路径与响应结构不变**，新能力放在 `/api/v2/*`。

```
POST   /api/login              GET  /api/session      POST /api/logout
GET    /api/csrf

GET    /api/domains            分页 + 搜索 + 状态筛选（旧版）
GET    /api/domain/{name}      POST /api/domain/check/{name}
POST   /api/domain/add         POST /api/domain/batch-add
DELETE /api/domain/remove/{name}
POST   /api/domain/whois-raw/{name}
GET    /api/stats              POST /api/monitor/{start,stop,reload}
GET    /api/settings           POST /api/settings/{smtp,telegram,bark,feishu,webhook,monitor}
POST   /api/notification/test  POST /api/test/{email,telegram}
POST   /api/database/clean-orphaned
GET    /health                 GET  /api/health/providers

GET    /api/domains/{domain}/history      状态时间线
GET    /api/domains/{domain}/attempts     各查询源的历史尝试

GET    /api/v2/overview        GET  /api/v2/meta       GET /api/v2/facets
GET    /api/v2/domains         支持 statuses=a,b,c 多状态并集
POST   /api/v2/domains         POST /api/v2/domains/{batch-add,batch-delete,batch-check}
GET    /api/v2/domains/{domain}           详情 + 证据 + 历史 + 尝试
PATCH  /api/v2/domains/{domain}           收藏 / 备注 / 标签 / 通知开关
DELETE /api/v2/domains/{domain}           POST /api/v2/domains/{domain}/check
GET    /api/v2/observations    GET  /api/v2/providers  GET /api/v2/notifications
POST   /api/v2/notifications/test/{channel}
GET    /api/v2/settings        PUT  /api/v2/settings/{query-policy,history,log-level}
GET    /api/v2/backups         POST /api/v2/backups

GET/POST/PUT/DELETE /api/v2/saved-views                 保存智能视图
POST   /api/v2/bulk-actions/preview                     批量动作影响预览
POST   /api/v2/bulk-actions                             批量安全动作执行
GET    /api/v2/bulk-actions/audits                      批量动作审计
GET/PUT /api/v2/ai/settings                             DeepSeek 设置（不回显 Key）
GET    /api/v2/ai/{models,usage,jobs,valuations/{domain}}
POST   /api/v2/ai/jobs                                  持久化 AI 估价 Job
GET/POST/PUT/DELETE /api/v2/automation/rules            规则构建器
POST   /api/v2/automation/rules/{id}/dry-run             Dry-run 与运行审计
GET    /api/v2/automation/runs
POST   /api/v2/automation/evaluate                       事件评估（默认不执行）
```

`GET /api/v2/facets` 返回**全量**的后缀 / 注册商 / 查询源 / 状态 / 标签清单及各自
数量——筛选下拉框据此渲染，翻页不会让可选项跟着变。

> 安全调整：`GET /api/settings` 不再回吐 SMTP 密码与 Telegram Bot Token 明文，
> 改为 `password_set` / `bot_token_set` 布尔值；保存时对应字段留空表示不修改。

## P1 高级筛选、AI 研究与自动化

P1 的高级条件以版本化 JSON 条件树编码进 URL，支持状态、TLD、注册商、标签、
日期和 AI 结果条件；智能视图只保存条件，不保存私密凭据。批量标签、优先级、
文件夹、通知、监控和 AI 估价都先通过 `/bulk-actions/preview` 返回实际匹配数、
样例、任务数、缓存命中和每日限额，再执行后端批量动作。

AI 支持多个 OpenAI-compatible 配置档案，默认显示提供商为 `OpenAI Compatible`；
新安装默认使用 `https://opencode.ai/zen/v1` 与 `deepseek-v4-flash-free`，并通过
`/api/v2/ai/providers` 管理多个配置。Job 写入 SQLite，
包含 `queued/running/succeeded/failed/deferred/cancelled` 状态、租约恢复、指数退避、
输入指纹去重、TTL 缓存和每日限额。发送给模型的输入只含域名、TLD、字符特征、
确认状态、可信度、日期、注册商及可选标签/优先级，绝不包含 WHOIS/RDAP 原文、备注、
通知配置、Token 或密码。模型返回的严格 JSON Schema 未通过时不会伪造估价。

密钥优先从 `DOMAINHUNTER_AI_API_KEY` 读取；UI 保存 Key 需要
`DOMAINHUNTER_SECRET_KEY`，数据库只保存 AES-GCM 密文。读取接口只返回
`api_key_set` 与 `key_source`。Base URL 默认仅 HTTPS，拒绝 loopback、私有/链路本地、
元数据地址，解析 DNS 后再次检查；`DOMAINHUNTER_AI_ALLOWED_HOSTS` 可收紧主机范围，
只有显式 `DOMAINHUNTER_AI_ALLOW_INSECURE_LOCAL=true` 才允许受控本机 HTTP。

自动化以“触发器 → 条件 → 安全动作 → 冷却/每日上限”构建，服务端只允许标签、
优先级、文件夹、通知开关、监控开关、加入 AI 队列、已有检查/通知等动作；规则不得
删除域名、改密码/密钥、支付购买或调用任意 URL。运行以 `(rule_id,event_id,domain)`
幂等，并带 per-domain cooldown、daily cap 和审计记录；新规则默认 Dry-run。后台事件桥接
会从新增域名、观测完成/状态变化/异常恢复和每日临近到期扫描投递事件，游标持久化在
SQLite；批量写入与 AI 入队均记录匹配数、任务数和结果，可从 `bulk-actions/audits` 查询。

## 开发

```bash
# 后端
go build ./... && go vet ./... && go test ./...
go test -race ./...

# 前端（源码在 web/）
cd web
npm ci
npm run lint
npm run build      # 产物写入 web/dist，需要一起提交
npm run dev        # 开发服务器，自动把 /api 代理到 127.0.0.1:8080
```

`web/dist` 随仓库提交并通过 `go:embed` 打进二进制，这样 `go build` 与 GoReleaser
在没有 Node 的环境下也能产出完整可用的程序，Docker 镜像也不需要 Node 阶段。
CI 会校验 `web/dist` 与前端源码一致。

## 测试

```bash
go test ./...          # 全部单元测试
go test -race ./...    # 竞态检测（需要 CGO 与 C 编译器）
```

覆盖重点：查询策略的每条决策分支、调度器的到期/优先级/热更新/优雅停止、
仓储的迁移与保留策略、认证的密码迁移。

## 备份与恢复

- 有待执行迁移时启动会自动 `VACUUM INTO` 生成备份，失败即中止升级。
- 管理端「系统设置 → 维护」可随时手动备份，只保留最近 5 份。
- 恢复：停容器 → 用备份覆盖 `data/domainhunter.db` → 删除同名的 `-wal` / `-shm`
  → 重启。

## 故障排查

| 现象 | 排查方向 |
| --- | --- |
| 大量域名显示 `skipped` | 该后缀没有 RDAP/WHOIS 端点；检查「查询源」页与 `servers.json` |
| 某后缀持续 `error` | 看域名详情的"查询证据"，确认是超时、限流还是解析失败 |
| `.im` 没有到期日 | 确认 `DOMAINHUNTER_WHOIS_LS_URL` / `_TLDS` 已配置且服务可达 |
| `.do` 显示 `unknown` | 确认本地 whois-domain-lookup 服务可达（`extra_hosts` 是否生效） |
| 反代下登录后立刻掉线 | 反代需要透传 `X-Forwarded-Proto`，或显式设置 `DOMAINHUNTER_COOKIE_SECURE` |
| 前端写操作 403 | CSRF 校验失败；刷新页面重新获取令牌，或检查反代是否吞掉了 `Origin` |
| 数据库越来越大 | 「系统设置 → 历史数据保留」下调心跳间隔以外的项，或确认心跳间隔不是 0 |
| `sqlite3` 命令行报 `database is locked` | 应用正在写。加 `-cmd '.timeout 15000'` 即可；应用自身有 10 秒 busy timeout，不受影响 |
| 退出登录后其他设备也要重新登录 | 这是有意的：退出与改密码都会轮换签名密钥，撤销所有设备的免登录令牌 |

## 发布

GitHub Release 发布后，`.github/workflows/goreleaser.yml` 生成跨平台归档，
`.github/workflows/docker.yml` 构建 `ghcr.io/nextcandy/domainhunter` 的
amd64/arm64 镜像。发布前至少执行：

```bash
gofmt -l $(git ls-files '*.go')
go vet ./... && go test ./...
(cd web && npm ci && npm run lint && npm run build)
docker compose config -q
```

## License

MIT License，详见 [LICENSE](LICENSE)。
