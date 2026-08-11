# DomainHunter 重构交接

> 最后更新：2026-08-11 · 分支 `refactor/domainhunter-v2` · 线上版本 `v2.1.0`

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
| 前端 | React 18 + TS + Vite + Tailwind，7 个页面，go:embed 进单容器 |
| 文档 | README / ARCHITECTURE.md / MIGRATION.md / 本文件 |
| 改名 | 仓库、镜像、树莓派部署目录与 compose 项目名统一为 DomainHunter |

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

新增 `schema_migrations` 并按版本执行 5 条迁移，**全部是新增，没有任何列被
重命名或删除**：

| 版本 | 内容 |
| --- | --- |
| 001 | baseline（老库自动标记为已应用，不重复建表） |
| 002 | `domains` 增加 priority / retry_count / next_check_at / favorite / note / tags / last_notified_status + 调度索引 |
| 003 | 新增 `domain_observations` |
| 004 | 新增 `query_attempts` |
| 005 | `app_settings` 预置 server_password_hash / session_secret |

因此 **v1 二进制仍能读 v2 的库**，这是回滚能无损进行的前提。

数据文件：全新安装创建 `domainhunter.db`；**已存在 `puff.db` 的部署继续使用
puff.db 且不会被改名**（树莓派上就是这种情况）。可用 `DOMAINHUNTER_DB_FILE`
覆盖。

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
- 内存从 817 个常驻 goroutine 降到 **10 线程 / 31MB RSS**

## 前端

React 18 + TypeScript + Vite + Tailwind v3，运行时依赖只有 react /
react-dom / react-router-dom，构建产物 gzip 后约 72KB，**零外部请求**。

7 个页面：概览 / 域名 / 观察列表 / 查询历史 / 查询源 / 通知 / 系统设置。
域名详情抽屉分四个标签页：概览 / 查询证据 / 状态时间线 / 原始报文。

`web/dist` 随仓库提交并 go:embed，因此 `go build` 与 GoReleaser 在没有 Node 的
环境下也能产出完整程序，Docker 镜像也不需要 Node 阶段。CI 会校验 dist 与源码一致。

## API

旧接口全部保留（路径与响应结构不变）：`/api/domains`、`/api/domain/*`、
`/api/stats`、`/api/settings/*`、`/api/monitor/*`、`/health`。

唯一的行为调整（安全原因）：`GET /api/settings` 不再回吐 SMTP 密码与 TG Token
明文，改为 `password_set` / `bot_token_set`；保存时留空表示不修改。

新增：`/api/domains/{domain}/history`、`/api/domains/{domain}/attempts`、
`/api/v2/{overview,meta,facets,domains,observations,providers,notifications,settings,backups}`、
`/api/v2/notifications/test/{channel}`、`/api/health/providers`。

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
未配置渠道给出明确提示                          PASS
/health                                        PASS
```

## 测试结果

```
go build ./...        PASS
go vet ./...          PASS
go test ./...         PASS（auth / httpapi / query / 4 个 provider /
                            scheduler / service / storage 共 9 个包）
go test -race ./...   PASS（在 GitHub Actions amd64 上）
前端 npm run lint     PASS
前端 npm run build    PASS（gzip 72KB）
docker compose config PASS
docker build          PASS（arm64 本机 + CI）
```

> `go test -race` **无法在树莓派上运行**：该机内核是 47 位 VMA，
> ThreadSanitizer 要求 48 位，会直接 `FATAL: unsupported VMA range`。
> 这是环境限制不是代码问题，因此 race 检测放在 CI（ubuntu amd64）上跑。
> 本机 Windows 也跑不了（`-race` 需要 CGO 与 C 编译器）。

## Docker 验证

```
docker compose -p domainhunter -f compose.yaml config -q   PASS
docker build --build-arg VERSION=v2.1.0 -t domainhunter:v2.1.0   PASS（arm64，37.5MB）
docker compose -p domainhunter up -d                        PASS
HEALTHCHECK                                                 healthy
```

## 当前部署状态

```
主机        树莓派 Pi (aarch64)，SSH 100.116.187.99:22370
容器        DomainHunter        镜像 domainhunter:latest (= v2.1.0)
端口        22334 → 8080        网络 domainhunter_default
compose     项目名 domainhunter，配置 /opt/docker-migrated/domainhunter/compose.yaml
数据目录    /opt/docker-migrated/domainhunter （即容器内 /app/data）
数据库      puff.db（15.5MB，771 个域名）—— 沿用旧文件名，程序自动识别
资源        31MB RSS / 10 线程 / CPU 接近 0
容器总数    35（与改动前一致，未影响任何其他项目）
```

## 回滚方式

按影响面从小到大：

**1. 只回滚程序（数据不动，推荐）**

```bash
cd /opt/docker-migrated/domainhunter
docker tag domainhunter-go:v1-rollback domainhunter:latest
docker compose -p domainhunter -f compose.yaml up -d
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
docker compose -p domainhunter -f compose.yaml stop
cp -a backups/puff-20260811-100852-pre-v2-manual.db puff.db
rm -f puff.db-wal puff.db-shm
docker compose -p domainhunter -f compose.yaml up -d
```

**回滚点清单**

```
分支        refactor/domainhunter-v2
tag         backup-before-domainhunter-refactor-20260811-0852
改动前 commit 1f989c7f02bf7303696112a217cdde4fa5313581
旧镜像      domainhunter-go:v1-rollback (31cc46c9b25f)
数据库备份  /opt/docker-migrated/domainhunter/backups/
              puff-20260811-100852-pre-v2-manual.db   （升级前，817 域名）
              puff-20260811-100918-pre-migration.db   （迁移前自动生成）
              puff-20260811-140520-pre-rename.db      （改名前，771 域名）
compose 备份 /opt/docker-migrated/domainhunter/compose.yaml.pre-rename
```

## 未完成项

- **`refactor/domainhunter-v2` 尚未合并到 `main`**。线上跑的就是这个分支的构建，
  但仓库默认分支仍是改名前的 `main`。建议 review 后开 PR 合并。
- 没有打 Release tag，因此 GHCR 上还没有 v2 镜像；树莓派用的是本地构建。
- `transfer_locked` 的语义未调整（见下）。
- 限速目前只有"最小间隔"，没有"每 Provider 并发上限"。`.do` 的本地服务在并发
  4 时会从 2 秒退化到 20 秒，间隔限速只能缓解不能根治。
- 前端已完成构建、lint、资源加载与登录页渲染验证，但**登录后的各页面没有由我
  人工逐页浏览过**（不便代输账号密码）。建议你自己点一遍确认观感。

## 已知问题

1. **605 个域名显示"转移锁定"而不是"已注册"**。这是重构前就有的行为：绝大多数
   正常 .com 都带 `clientTransferProhibited`，而状态映射把 Hold/转移锁定排在
   registered 之前。**没有改动**，因为改了会让 605 个域名产生一次状态变化通知。
   建议后续把"转移锁定"降级成标记而不是主状态（详情页已经能看到 EPP 原始状态）。

2. **`.do` 域名容易变成 unknown/error**。本地 whois-domain-lookup 服务单实例，
   并发压力下从 2.8 秒退化成 20–90 秒超时。已加 1s 最小间隔缓解。

3. **`epp_statuses` 不会持久化**，只在刚查完的那次响应里出现；
   `domain_results` 表没有这一列。

4. **`com.cn` / `net.cn` / `org.cn` 会被归类成 `cn`**，因为内置
   `servers.json` 没有这几个二级后缀条目。筛选清单与筛选行为是一致的，
   只是无法单独筛出 com.cn。

5. `web/node_modules` 里有个 flatted 包自带 Go 源码，会被 `go build ./...`
   扫到（无害，CI 里两个 job 是分开的）。

## 下一步建议

1. Review 后把分支合并进 `main`，打 `v2.1.0` tag 触发 GHCR 镜像与 Release。
2. 给 `fallback` / `whois_ls` 加"每 Provider 并发上限"，比最小间隔更能保护
   单实例服务。
3. 把"转移锁定"从主状态改成附加标记，让 605 个域名回到"已注册"——需要一次性
   抑制通知。
4. 考虑把 `puff.db` 改名成 `domainhunter.db`（程序已支持两种名字）：
   停容器 → `mv puff.db domainhunter.db` → 起容器。回滚时改回即可。
5. 历史表已有 1.9 万条观测；若想立刻瘦身，可把「每域名最多保留」调小后等一次
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
  46 个域名是用户删的而不是程序出错。已补。
- **每次查询都写一条观测**会让库 4 小时从 5.7MB 涨到 15MB。已改成"状态变化或
  超过心跳间隔才记"。
- **升级后 817 个域名同时到期**会把注册局和本地服务打爆，导致本来能查到的
  `.do` 域名变成 unknown。已改成补齐调度时间时均摊到一个间隔窗口。
- **GitHub 上 `NextCandy/DomainHunter` 原本重定向到 `NextCandy/dh`**（那个
  TypeScript 项目以前叫 DomainHunter）。本次改名占用了该路径，`dh` 的旧链接
  失效了。
- **PowerShell 不支持 heredoc**：给树莓派传脚本要用 base64 编码，直接拼字符串
  会被反复转义搞坏。
