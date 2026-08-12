# DomainHunter 重构交接

> 最后更新：2026-08-13 · 圆润卡片、AI 限流重试与 DeepSeek 官方 V4 Flash 配置已部署 · Pi 版本 `v2.9.1-card-ai-20260813`

## 本次目标

在保持核心功能与数据兼容的前提下，把 DomainHunter 从"单体 + 全局函数"重构成
分层结构：查询源可插拔、调度可控、数据有历史、认证可用、前端可维护，并且继续
保持单容器 / SQLite / ARM64 / 树莓派友好。

## 原项目状态

- 8 个包约 8000 行 Go + 2600 行原生前端，2 个 commit
- 查询顺序、`.im`/`.do` 特判硬编码在 `DomainChecker.CheckDomain` 一个函数里
- **每个域名一个常驻 goroutine + ticker**（817 个域名 = 817 个协程各自轮询数据库）
- 重试逻辑重复两份；无 Repository 层；`config` 反向依赖 `storage`
- 只有当前状态快照，没有任何历史
- 密码明文存库，`/api/settings` 明文回吐 SMTP 密码与 TG Token
- Cookie `Secure: false` 硬编码、`Access-Control-Allow-Origin: *` 全局、无 CSRF
- 无 schema migration，靠启动时零散 `ALTER TABLE`
- 登录页从 CDN 加载 Tailwind/daisyUI，断网即样式全失

## 完成内容

| 领域 | 结果 |
| --- | --- |
| 查询引擎 | Provider 接口 + 可按 TLD 配置的 QueryPolicy + Evidence/Confidence + 分类错误模型 |
| 调度 | next_check_at 驱动的调度循环 + 三级优先级队列 + 固定 worker pool |
| 存储 | Repository 接口 + SQLite 实现 + schema migration + VACUUM INTO 自动备份 |
| 历史 | domain_observations / query_attempts + 保留策略 + 心跳去重 |
| 安全 | bcrypt + 旧密码自动迁移、Cookie 加固、CORS 收敛、CSRF、退出即撤销免登录令牌 |
| 通知 | 邮件 / Telegram / **Bark** / **飞书机器人** / **自定义 Webhook** |
| 前端 | React 18 + TS + Vite + Tailwind，7 个页面，文件夹/导入导出/通知规则/Token 管理，go:embed 进单容器 |
| P1 筛选与批量 | 版本化高级筛选树、可分享智能视图、批量动作影响预览与单事务安全执行 |
| P1 AI | 兼容版多 Provider 配置档案、自动化、AES-GCM Key 密文、SSRF 防护、持久化 Job/租约/重试/限额/TTL 估价 |
| 严格 AI 估价 | DeepSeek 研究性估价、最小化输入、严格 JSON schema、复核门禁、档案级并发、缓存、配额和审计 |
| 复核与健康 | 低可信度/未知/冲突/过期证据复核，Provider 错误率、P50/P95、连续失败和离线原因 |
| UI 美化 | 工作台行动队列、复核徽标、浅色/深色主题、响应式估价面板与四行 AI 鉴定报告 |
| P1 自动化 | 触发器/条件/白名单动作/冷却/每日上限、Dry-run、事件桥接、幂等运行审计与批量审计 |
| 文档与图标 | README / ARCHITECTURE.md / MIGRATION.md / 本文件；`web/public/DomainHunter.svg` 与 `.png` |
| 改名 | 仓库、镜像、树莓派部署目录与 compose 项目名统一为 DomainHunter |

Bark 通知使用精简正文：去掉 WHOIS/RDAP 原文、详细信息和自动页脚，仅保留状态摘要，正文约 480 字节以内。

## 架构变化

```
重构前：main → core.Monitor → WorkerManager → 每域名一个 DomainWorker
                                   ↓
                          DomainChecker（写死四个查询源顺序）
        web.handlers(1489 行) → 直接调 storage 全局函数

重构后：cmd/domainhunter → internal/{domain,registry,query,scheduler,service,
                                      repository,storage,notification,auth,
                                      config,httpapi,httpx,logger}
        HTTP → Service → Scheduler → Worker Pool → Query Engine → Provider
                  ↓                                      ↓
             Repository ←────────────────────────── SQLite
```

依赖方向严格由外向内，`internal/domain` 不依赖任何其他内部包。详见
[ARCHITECTURE.md](ARCHITECTURE.md)。

## 数据库变化

新增 `schema_migrations` 并按版本执行 16 条迁移；结构迁移不删除任何列，016 只把旧的
OpenCode 默认档案更新为 DeepSeek 官方配置：

| 版本 | 内容 |
| --- | --- |
| 001 | baseline（老库自动标记为已应用，不重复建表） |
| 002 | `domains` 增加 priority / retry_count / next_check_at / favorite / note / tags / last_notified_status + 调度索引 |
| 003 | 新增 `domain_observations` |
| 004 | 新增 `query_attempts` |
| 005 | `app_settings` 预置 server_password_hash / session_secret |
| 006 | `domain_results.epp_statuses`，保存 EPP 附加状态 |
| 007 | `folders` 与 `domains.folder_id` |
| 008 | `api_tokens` |
| 009 | `notification_rules` / `notification_templates` / `notification_digest` |
| 010 | P1 `saved_views` / `ai_provider_settings` / `ai_jobs` / `ai_domain_valuations` / `automation_rules` / `automation_runs` |
| 011 | P1 `automation_cursors` / `bulk_action_audits`，事件游标与批量审计 |
| 012 | `ai_provider_profiles` 多 AI 配置档案，并把新安装默认项设为 OpenAI Compatible |
| 013 | 严格研究性估价的 `ai_profiles` / `ai_valuation_jobs` / `ai_domain_valuations_v2` / `ai_audit_log` |
| 014 | `domain_results.confidence` 可信度字段 |
| 015 | 严格 AI 人民币价格区间与核心分析字段 |
| 016 | 默认 AI 档案切换到 `api.deepseek.com` / `deepseek-v4-flash` |

因此 **v1 二进制仍能读 v2 的库**；迁移 010–015 仅新增表和索引/字段，旧字段与旧 API 不变，
回滚程序不会删除 P1 数据。

数据文件：默认 `domainhunter.db`；**已存在 `puff.db` 的部署继续使用 puff.db，
程序绝不自动改名**。树莓派已在 2026-08-11 停机手工改名为 `domainhunter.db`。
可用 `DOMAINHUNTER_DB_FILE` 覆盖。

## Query Provider

四个 Provider 实现同一接口，统一返回 `query.Result`：

| Provider | 类型 | 说明 |
| --- | --- | --- |
| `rdap` | 通用 | 结构化优先；只有合法 RDAP 错误对象 + 明确"不存在"语义才判可注册 |
| `whois` | 通用 | 注册局 43 端口 |
| `whois_ls` | 按后缀启用 | 部署上用于 `.im` |
| `fallback` | 按后缀启用 | 本地 whois-domain-lookup，部署上用于 `.im` / `.do` |

**available 采信程度**可按 TLD 配置：`trust` / `confirm`（需二次印证）/
`distrust`（不单独采信）。默认行为与重构前**逐条一致**：该后缀启用了专用源时，
通用源的 available 不被单独采信。

安全判定全部保留并补了测试：HTTP 404 语义不明、超时、连接失败、注册局策略文本、
空 RDAP 对象、结构化标志不完整 —— 一律不可能变成 available。

`whois_ls` / `fallback` 默认加 1s 最小查询间隔（`rate_limits` 可调）。

## Scheduler

```
domains.next_check_at → 每 5s 扫描 → 优先级队列(manual>retry>scheduled)
                                        → N 个 worker → QueryService
```

- 手动"立即检查"以 manual 优先级插队，与定时查询共用同一条 Pipeline
- 间隔：正常状态用设置里的检查间隔；error 退避 5min→15min→1h；skipped 24h
- 升级首次补齐 next_check_at 时把已到期域名**均摊到一个间隔窗口**，避免几百个
  域名同一秒全部到期
- 每日摘要独立于查询调度器运行，按配置时间聚合前一天变化；发送失败不推进
  `last_sent_at`，成功或无变化才去重记录
- 内存从 817 个常驻 goroutine 降到 **10 线程 / 31MB RSS**

## 前端

React 18 + TypeScript + Vite + Tailwind v3，运行时依赖只有 react /
react-dom / react-router-dom，当前构建产物 JS gzip 后约 94KB，**零外部请求**。

8 个页面：概览 / 域名 / 抢注看板 / 查询历史 / 查询源 / 通知 / 自动化与 AI / 系统设置。
域名详情抽屉分四个标签页：概览 / 查询证据 / 状态时间线 / 原始报文。

**抢注看板**只显示处于掉落流程的域名（可注册 / 待删除 / 赎回期 / 已过期 /
宽限期），按抢注紧迫度排序。它取代了原来的「观察列表」——那一页本质上就是
「域名列表 + favorite=true」，同一个组件加一个 prop，且收藏数一直是 0。
收藏能力保留为域名页筛选栏里的「★ 只看收藏」开关。

`web/dist` 随仓库提交并 go:embed，因此 `go build` 与 GoReleaser 在没有 Node 的
环境下也能产出完整程序，Docker 镜像也不需要 Node 阶段。CI 会校验 dist 与源码一致。

## API

旧接口全部保留（路径与响应结构不变）：`/api/domains`、`/api/domain/*`、
`/api/stats`、`/api/settings/*`、`/api/monitor/*`、`/health`。

唯一的行为调整（安全原因）：`GET /api/settings` 不再回吐 SMTP 密码与 TG Token
明文，改为 `password_set` / `bot_token_set`；保存时留空表示不修改。

新增：`/api/domains/{domain}/history`、`/api/domains/{domain}/attempts`、
`/api/v2/{overview,meta,facets,domains,observations,providers,notifications,settings,backups}`、
`/api/v2/notifications/test/{channel}`、`/api/health/providers`、P1 的
`saved-views`、`bulk-actions`、`ai/{settings,models,usage,jobs,valuations}`、
`automation/{rules,runs,evaluate}`、`bulk-actions/audits`。

## 安全调整

- 密码改 bcrypt；旧明文密码**首次登录成功时自动迁移**，明文行暂时保留以保证
  升级当天可无损回滚，用户下次改密码时清空
- "记住登录"令牌改用独立的 `session_secret` 签名；**退出登录与修改密码都会
  轮换该密钥**，使所有设备上的免登录令牌立即失效
- Cookie 支持 `Secure`(auto/true/false) 与 `SameSite`，auto 会识别反代的
  `X-Forwarded-Proto`
- 去掉全局 `Access-Control-Allow-Origin: *`，改为按需配置
- 写操作增加 CSRF 校验（Origin / Sec-Fetch-Site / 双提交令牌三选一）

## 已验证

**本机**：`go build` / `go vet` / `go test` 全通过；实际启动后完成登录、CSRF
拒绝、批量添加、调度自动查询、详情、历史、raw、设置读取、监控启停、前端资源
加载与主题/响应式检查。

**GitHub CI**：Go（含 `go test -race`）、前端 lint+build+dist 一致性、Docker
构建 —— 三个 job 全绿。

**树莓派实测**：

```
登录（原账号原密码，明文自动迁移为哈希）      PASS
未登录访问受保护接口 → 401                    PASS
伪造 Origin 的写操作 → 403                    PASS
退出登录 → 再访问 401（旧 remember 令牌失效）  PASS
域名列表 / 分页 / 搜索 / 状态筛选              PASS
后缀精确筛选（cn 不含 com.cn）                 PASS
完整后缀清单 34 种 + 注册商 155 种             PASS
添加测试域名 → 立即检查 → 删除 → 数据清零      PASS
.com/.net → rdap    .im → whois_ls    .do → fallback   PASS
详情 / 观测历史 / 查询尝试 / 原始报文          PASS
Bark 测试推送（真机收到）                      PASS
email / Telegram / Bark 真实测试（2026-08-12）  PASS
Bark 精简正文回归测试                            PASS
未配置渠道给出明确提示                          PASS
/health                                        PASS
P1 migration 010–012 / 新增表与索引                     PASS
严格 AI migration 013–014 / integrity / 545 domains    PASS
严格 AI 未登录策略接口 → 401                            PASS
严格 AI Key 不回显 / 研究性状态 guard                    PASS（代码与单测）
ARM64 image build / version v2.6.0-strict-ai-20260812-d346a1f PASS
health / schema 014 / integrity / 545 domains             PASS
P1 未登录访问 AI/视图/高级列表接口 → 401                  PASS
P1 未登录访问批量审计接口 → 401                           PASS
P1 多 AI 配置默认项 / Provider models 200                PASS
P1 前端 go:embed 图标与 AI 配置表单                       PASS
```

## 测试结果

```
go build ./...        PASS
go test ./...         PASS（含 internal/p1 专项测试）
go test -race ./...   PASS（本机 macOS；Pi 仅运行镜像）
go vet ./...          PASS
前端 npm run lint     PASS
前端 npm run build    PASS（gzip JS 94KB）
前端 npm ci            PASS（npm audit 报现有依赖树 4 个漏洞，未执行 audit fix）
docker compose config PASS
docker build          PASS（Pi arm64，镜像 `domainhunter:v2.6.0-strict-ai-20260812-d346a1f`）
隔离容器 smoke        PASS（/health 200、版本正确、迁移 013/014 应用）
browser QA             PASS（本地隔离环境，浅色/深色、响应式与 AI 配置交互可见）
GitHub Actions         PASS（Go / Frontend / Docker，commit `d346a1f`）
```

> `go test -race` **无法在树莓派上运行**：该机内核是 47 位 VMA，
> ThreadSanitizer 要求 48 位，会直接 `FATAL: unsupported VMA range`。
> 这是环境限制不是代码问题，因此 race 检测放在 CI（ubuntu amd64）上跑。
> 本机 Windows 也跑不了（`-race` 需要 CGO 与 C 编译器）。

## Docker 验证

```
docker compose -p domainhunter -f compose.yaml config -q   PASS
docker build --build-arg VERSION=v2.6.0-strict-ai-20260812-d346a1f -t domainhunter:v2.6.0-strict-ai-20260812-d346a1f   PASS（Pi arm64）
docker compose -p domainhunter -f compose.yaml up -d --no-build --no-deps domainhunter PASS
HEALTHCHECK                                                 healthy
`/health`                                                     HTTP 200，status=ok，545 domains
旧 `/api/session`                                            HTTP 200，未登录受保护 v2 AI 策略接口 HTTP 401
restart=0 / OOM=false                                        PASS
```

## 当前部署状态

```
主机        树莓派 Pi (aarch64)，SSH 192.168.50.180:22370
容器        DomainHunter        镜像 domainhunter:v2.6.0-strict-ai-20260812-d346a1f
端口        22334 → 8080        网络 domainhunter_default
compose     项目名 domainhunter，配置 /opt/docker-migrated/domainhunter/compose.yaml
数据目录    /opt/docker-migrated/domainhunter （即容器内 /app/data）
数据库      domainhunter.db（约17MB，545 个域名）—— 2026-08-11 停机由 puff.db 改名
资源        内存限制 0 / cpu_shares 512 / 重启 0 / 当前健康
容器总数    35（与改动前一致，未影响任何其他项目）
备份        backups/domainhunter-20260812-221928-pre-d346a1f/domainhunter.db（integrity=ok）
Compose备份 compose.yaml.pre-d346a1f-20260812-222349
回滚镜像    domainhunter:rollback-pre-d346a1f-20260812-221928
运行时配置  `.env`（权限 600；API Key 未写入 Git 或数据库明文）
```

## 回滚方式

按影响面从小到大：

**1. 只回滚程序（数据不动，推荐）**

```bash
cd /opt/docker-migrated/domainhunter
docker tag domainhunter:rollback-pre-d346a1f-20260812-221928 domainhunter:v2.6.0-strict-ai-20260812-d346a1f
docker compose -p domainhunter -f compose.yaml up -d --no-build --no-deps domainhunter
curl -f http://127.0.0.1:22334/health
```

迁移只做加法，v1 能直接读现在的库；登录用原密码（明文行仍在）。

**2. 回滚到改名前的目录与项目名**

```bash
docker compose -p domainhunter -f /opt/docker-migrated/domainhunter/compose.yaml down
mv /opt/docker-migrated/domainhunter /opt/docker-migrated/puff
cd /opt/docker-migrated/puff && cp -a compose.yaml.pre-rename compose.yaml
docker compose -p puff -f compose.yaml up -d
```

**3. 连数据一起回滚**

```bash
cd /opt/docker-migrated/domainhunter
docker compose -p domainhunter -f compose.yaml stop
cp -a backups/puff-20260811-100852-pre-v2-manual.db domainhunter.db
rm -f domainhunter.db-wal domainhunter.db-shm
docker compose -p domainhunter -f compose.yaml up -d
```

**4. 只回滚数据库文件名**

```bash
cd /opt/docker-migrated/domainhunter
docker compose -p domainhunter -f compose.yaml stop
mv domainhunter.db puff.db
docker compose -p domainhunter -f compose.yaml up -d
```

**回滚点清单**

```
分支        main（refactor/domainhunter-v2 已快进合并进来并删除）
tag         backup-before-domainhunter-refactor-20260811-0852  ← 回滚到重构前用它
改动前 commit 1f989c7f02bf7303696112a217cdde4fa5313581
旧镜像      domainhunter-go:v1-rollback (31cc46c9b25f)  ← v1
            domainhunter:v2.1.0-rollback、domainhunter:v2.2.0  ← v2 各阶段
            domainhunter:v2.4.0-rollback-20260812-0003  ← Bark 修复前
            domainhunter:rollback-pre-d346a1f-20260812-221928  ← v2.6.0 部署前
数据库备份  /opt/docker-migrated/domainhunter/backups/
              domainhunter-20260812-070555-pre-mobile.db  （移动端响应式修复部署前）
              domainhunter-20260812-0003-pre-bark.db
              puff-20260811-100852-pre-v2-manual.db      （重构前基线，817 域名）
              puff-20260811-143159-pre-owned-cleanup.db  （清理已拥有域名前，695）
              puff-20260811-153045-pre-db-rename.db      （改名前冷拷贝，551）
              domainhunter-20260812-221928-pre-d346a1f/domainhunter.db（v2.6.0 部署前）
compose 备份 /opt/docker-migrated/domainhunter/compose.yaml.pre-rename
             /opt/docker-migrated/domainhunter/compose.yaml.pre-dbrename-comment
             /opt/docker-migrated/domainhunter/compose.yaml.pre-bark-20260812-0003
             /opt/docker-migrated/domainhunter/compose.yaml.pre-d346a1f-20260812-222349
```

## 未完成项

- 飞书机器人与自定义 Webhook 当前未启用，因此未发送真实测试消息。

## 已知问题

1. **旧库里的转移锁定快照会逐步收敛**。新查询已把
   `client/serverTransferProhibited` 降级为 EPP 附加标记并将主状态判为
   `registered`，不会为这次兼容性回归重复通知；现网旧结果随正常调度重查，
   不做高并发一次性批量重查。

2. **`.do` 域名容易变成 unknown/error**。本地 whois-domain-lookup 服务单实例，
   并发压力下从 2.8 秒退化成 20–90 秒超时。已加 1s 最小间隔缓解。

3. `web/node_modules` 里有个 flatted 包自带 Go 源码，会被 `go build ./...`
   扫到（无害，CI 里两个 job 是分开的）。

## 下一步建议

1. 可按发布流程打 Release tag，触发 GoReleaser 与 GHCR 多架构镜像。
2. 历史表已有 1.9 万条观测；若想立刻瘦身，可把「每域名最多保留」调小后等一次
   清理（每 6 小时一轮）。

## 踩坑记录

- **树莓派跑不了 `go test -race`**：47 位 VMA，TSan 要 48 位，直接 FATAL。
  race 检测只能放 CI。
- **`.gitignore` 里的 `dist/` 会连 `web/dist` 一起忽略**，导致 go:embed 的前端
  产物进不了仓库。必须写成 `/dist/`。
- **`golang.org/x/crypto` 最新版要求 Go ≥ 1.25**，与本项目的 1.24 不兼容，
  固定在 `v0.43.0`。
- **退出登录曾经形同虚设**：新前端调 `/api/logout`，但路由只注册了 `/logout`，
  请求落到 SPA 兜底返回 404；前端捕获异常后照样清本地状态，看起来"退出成功"，
  服务端会话其实还在。已修 + 加测试。
- **改密码后旧的"记住登录"令牌仍然有效**：重构把签名密钥从密码换成独立
  session_secret 时引入的回归。已改为退出/改密码都轮换密钥。
- **删除操作没有日志**是重构丢的：核对数据时只能靠和备份做差集，才能确认少掉的
  46 个域名是站主自己删的（已确认）而不是程序出错。已补。
- **每次查询都写一条观测**会让库 4 小时从 5.7MB 涨到 15MB。已改成"状态变化或
  超过心跳间隔才记"。
- **升级后 817 个域名同时到期**会把注册局和本地服务打爆，导致本来能查到的
  `.do` 域名变成 unknown。已改成补齐调度时间时均摊到一个间隔窗口。
- **PowerShell 不支持 heredoc**：给树莓派传脚本要用 base64 编码，直接拼字符串
  会被反复转义搞坏。
- **`docker build` 忘了 `--build-arg VERSION=` 会把版本号退回 `v2.0.0`**：
  dockerfile 的 `ARG VERSION=v2.0.0` 是兜底默认值，`-X main.AppVersion` 取的
  就是它。构建后一定要 `curl /health` 看 `version` 字段对不对。
- **PowerShell 的 `-Encoding UTF8` 会写 UTF-8 BOM**：给 `.go` 文件加 BOM 会让
  `gofmt -l` 报未格式化。改文件用 Edit 工具，不要用 `Set-Content -Encoding UTF8`。
- **Windows 检出是 CRLF（`core.autocrlf=true`）**，踩了两次：
  1. 把工作区打包传到树莓派跑 `gofmt -l .` 会把所有 `.go` 都列出来，
     真正的格式问题淹没在噪音里。校验前先把 `\r\n` 归一化成 `\n`。
  2. vite 生成的 `web/dist/index.html` 带 CRLF 进了索引（git 把它判成
     `-text` 所以 autocrlf 没转），CI 在 Linux 上重新 build 出的是 LF，
     "dist 是否与源码一致"整文件报差异。已加 `.gitattributes`
     （`* text=auto eol=lf`）根治。
