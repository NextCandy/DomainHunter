# DomainHunter 当前交接说明

## 当前范围

本仓库是独立的 DomainHunter Go/React 项目，运行形态为单体 Go 应用、SQLite 和单容器。前端构建产物位于 `web/dist`，由 `go:embed` 编入二进制。

## 生产部署

> 2026-08-16 起生产实例已从树莓派迁到 Synology NAS，下表是 NAS 上的实际值。

| 项目 | 当前值 |
| --- | --- |
| 宿主 | Synology NAS（Tailscale `100.99.27.142`） |
| 实例目录 | `/volume1/docker/domainhunter`（挂 `/app/data`；**已不再是数据目录**，只放 `.env`、备份与归档，运行时不读写） |
| 源码/构建目录 | `/volume1/docker/domainhunter-src`（**是 git clone**，remote 指向 GitHub，`git pull` 取最新） |
| Compose 文件 | `docker-compose.yml` |
| 容器 | `DomainHunter` |
| 当前镜像 | `domainhunter:v2.17.0-desk`（决策台改版，见 CHANGELOG-desk.md） |
| 回滚镜像 | `domainhunter:v2.12.0-anthropic-r7`（暖纸张版，上一代设计）／`-r5`、`-r6`；`v2.13.0-amber-r1`、`-r2`（本代中间版） |
| 服务端口 | `22334 → 8080` |
| 数据库 | `/volume4/domainhunter-db/domainhunter.db`（NVMe，挂 `/app/db`，见下「数据库位置」） |
| who-dat | `59090 → 8080` |
| whois（结构化兜底） | `12121 → 80`，容器名 `whois` —— **不是**旁边那个 `whois-domain-lookup`:30123 |
| Vercel who-dat | `https://whox.vercel.app/`（原默认 `rdap.re` 已失效，见「外部查询源」） |
| docker 二进制 | `/usr/local/bin/docker`、`/usr/local/bin/docker-compose`（不在 sudo 默认 PATH） |
| CPU shares | `512` |
| 内存上限 | 不设置 |

DomainHunter 镜像升级不会重建两个独立查询服务。生产切换前备份 Compose 和 SQLite，并先通过 `docker compose config -q`。

### 数据库位置（2026-09-12 迁到 volume4）

库**不在** `/app/data`。`volume1` 是机械盘，btrfs 的 fsync 慢到能让写锁超过 `busy_timeout` 的
15 秒并抛 `SQLITE_BUSY`，所以库迁到了 `volume4` 的 NVMe：

| | |
| --- | --- |
| 宿主路径 | `/volume4/domainhunter-db/`（`domainhunter.db` + `-wal` + `-shm`） |
| 容器挂载 | `/app/db` |
| 切换开关 | `DOMAINHUNTER_DATA_DIR: /app/db` |

> **坑**：`DOMAINHUNTER_DB_FILE` 只接受**相对文件名**，填绝对路径会开不了库。
> 迁移前那份 50MB 的旧库压缩后存在 `backups/legacy/domainhunter.db.pre-volume4-20260912-2219.gz`。

### 访问入口

| 途径 | 地址 | 状态 |
| --- | --- | --- |
| 日常（推荐） | `http://100.99.27.142:22334`（Tailscale） | 正常 |
| frp 隧道 | proxy `NAS_domainhunter_v2`：本地 22334 → 反代机 `195.245.241.61` 远程 **22336** | start success，但外网直连 22336 超时（疑似对端防火墙未放行 22336） |
| ~~`dshunter.com`~~ | — | **已失效**，该域名现为 Spaceship 域名出售停放页，不再指向本应用 |

frpc 配置在 `/volume1/docker/frpc/frpc.txt`（挂进容器为 `/frp/frpc.toml`），改完需
`docker restart frpc`（无 admin API，不能热重载；重启会短暂中断该机全部 frp 入口）。

> 2026-09-18 删掉了死 proxy `domainhunter_web`（本地 8832 → 远程 18833）：8832 早已没有任何
> 服务监听，是 v1 时代的遗留。删后 27 个 proxy 全部 `start proxy success`。

## 当前产品能力

- RDAP-first，多 Provider 证据合并和保守可注册判定。
- `.im` 只在至少两个来源确认时判可注册；到期日可有、注册日期保持为空。
- 可选 Spaceship API 只补充 `.im` 的可用/不可用事实。
- who-dat 与结构化 whois 服务是**同一台 NAS 上的本地查询源**，容器内经 `host.docker.internal` 访问。
- DeepSeek 官方 OpenAI-compatible 默认档案：`https://api.deepseek.com` + `deepseek-v4-flash`。
- AI 只输出评分、人民币价格区间和核心分析，不改变域名生命周期结论。
- 炭黑 × 琥珀橙的精密仪表盘 UI（2026-09-18 全量改版，见下「UI 设计系统」）。
- 桌面侧栏与移动底部导航，表格窄屏转卡片。

## 发布门禁

```bash
go test ./...
go vet ./...
cd web
npm run typecheck
npm run lint
npm run build
cd ..
git diff --check
docker compose config -q
```

确认 `web/dist` 已更新后再提交。不要提交 `.env`、API Key、SQLite 数据库或备份文件。

## 部署门禁（NAS 实操流程）

NAS 上没有 Node 也没有本地 Go 工具链，全部通过容器完成。**注意 SSH 会话一断就会杀掉前台的
`docker build`**，Go 编译必须走 detached 容器再轮询，不要在 SSH 前台直接跑完整多阶段构建。

**源码目录已经是 git clone（2026-09-20 起）**，取代码不再靠 tar/scp：

```bash
cd /volume1/docker/domainhunter-src && git pull
```

`web/dist` 随仓库提交，所以**改了前端要先在开发机 `npm run build` 并把 dist 一起提交**；
NAS 侧只在需要重新生成 dist 时才跑下面第 1 步。

```bash
# 1) 前端：在 node 容器内构建（node_modules 放容器层，避免 NAS 挂载卷的小文件开销）
docker run --rm -v /volume1/docker/domainhunter-src/web:/src \
  -e npm_config_registry=https://registry.npmmirror.com node:22-bookworm-slim bash -c '
  mkdir -p /build && cp -a /src/. /build/ && rm -rf /build/dist /build/node_modules && cd /build
  npm ci --no-audit --no-fund && npm run typecheck && npm run lint && npm run build
  rm -rf /src/dist && cp -r /build/dist /src/dist'

# 2) Go 二进制：detached 容器 + 轮询，参数与 dockerfile 的 builder 阶段一致
docker run -d --name dh-gobuild \
  -v /volume1/docker/domainhunter-src:/build \
  -v /volume1/docker/_backups/gomodcache:/go/pkg/mod -w /build \
  -e GOPROXY=https://goproxy.cn,direct -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH=amd64 \
  golang:1.26-alpine \
  sh -c 'go build -trimpath -ldflags "-s -w -X main.AppVersion=vX.Y.Z" -o /build/domainhunter-bin ./cmd/domainhunter'

# 3) 运行镜像：dockerfile.runtime 只 COPY 二进制，与官方 dockerfile 的 runtime 阶段等价
docker build -f dockerfile.runtime -t domainhunter:vX.Y.Z-tag .
docker run --rm domainhunter:vX.Y.Z-tag ./domainhunter -v

# 4) 切换（备份用 backup-db.sh，它走 VACUUM INTO，不必停容器；详见「备份」）
cd /volume1/docker/domainhunter
cp docker-compose.yml _archive/docker-compose.yml.pre-<tag>-$(date +%Y%m%d-%H%M%S)
sed -i 's|image: domainhunter:.*|image: domainhunter:vX.Y.Z-tag|' docker-compose.yml
docker-compose -f docker-compose.yml config -q
./backup-db.sh                          # 切换前留一份，日志进 backups/backup.log
docker-compose -f docker-compose.yml up -d
curl -f http://127.0.0.1:22334/health   # 库搬到 NVMe 后冷启约 6 秒（原 13 秒），过早 curl 会拿到 000
```

部署后记录：镜像 ID、health、restart count、OOMKilled、SQLite integrity、域名数量、关键 API 响应和容器日志。

## 回滚

优先把 Compose 镜像改回部署前的 rollback tag，再只重建 DomainHunter。数据库恢复仅在确认数据或迁移异常时执行，恢复前必须停容器并再次核对备份的 `PRAGMA integrity_check`。

## 备份（2026-09-18 建立自动备份）

在此之前 **没有任何自动备份**，最后一份手工快照停在 2026-08-17。现在：

| | |
| --- | --- |
| 脚本 | `/volume1/docker/domainhunter/backup-db.sh` |
| 调度 | `/etc/crontab` 每天 **04:30** |
| 日志 | `backups/backup.log` |
| 产物 | `backups/domainhunter-auto-<ts>.db.gz`，约 14MB／份，全库 7 秒跑完 |
| 保留 | 最近 **14** 份自动轮转（只匹配 `domainhunter-auto-*` 前缀，不动手工快照） |

**为什么用 `VACUUM INTO` 而不是 `.backup` 或 `cp`**：WAL 模式下 `cp` 单个 `.db`
会漏掉未 checkpoint 的 WAL，拿到的是旧快照；三个文件一起 `cp` 又不是原子的；
而 `sqlite3` 的 `.backup` 会和调度器的写入撞成 `database is locked`（2026-08 踩过）。
`VACUUM INTO` 是原子的、WAL 下读不阻塞写，产出已整理紧凑的单文件且不带 WAL/shm —— **不用停容器**。
脚本跑完校验 `PRAGMA integrity_check`，不为 `ok` 就删掉坏备份并退出非 0。

**群晖 cron 的两个坑**：

- `/etc/crontab` 必须 **TAB 分隔 + 带 user 字段 + 权限 644**，否则 crond 会整份拒绝加载
  （连原有任务一起失效）。改完用 `cat -A` 确认分隔符显示为 `^I`。
- 这台机器**没有 `synoservicectl`**（只有 `/usr/syno/bin/synosystemctl`），
  重启 crond 用 **`systemctl restart crond`**。

## 目录卫生约定

实例目录曾堆积 31 个 `compose.yaml.pre-*` / `.env.*` 散件。现在的约定：

| 放什么 | 放哪 |
| --- | --- |
| Compose／配置的历史副本 | `_archive/`（**不要再往项目根堆 `.pre-*`**） |
| 数据库自动备份 | `backups/` |
| 退役的历史库、损坏库快照 | `backups/legacy/`（一律 gzip） |

含明文密钥的归档包要 `chmod 600`。2026-09-18 一次性精简：实例目录 **466MB → 149MB**
（`backups/` 里 18 份未压缩的 `.db` 全部 gzip，无损、目录结构与 health/inspect 元数据原样保留；
`releases/` 只留最近 3 份源码树，其余打包进 `_archive/releases-old-*.tgz`）。

> `releases/` 里每份是**完整源码树**而非构建产物。真正的回滚载体是 docker 镜像 tag，
> 源码另有上游仓库和本构建目录各一份 —— 那个归档包其实冗余，可随时删。

## UI 设计系统（2026-09-18 全量改版）

从上一代的「Anthropic 暖纸张 + 衬线编辑腔」换成**炭黑 × 琥珀橙的精密仪表盘**。
**深色是默认主题**（`theme.tsx` 的默认值 + `index.html` 首屏脚本都是 `dark`），
亮色是可切换的镜像。

### 三条铁律

1. **发丝线，不要投影** —— 所有分隔只用 1px 发丝线（`--line`），全站零 `box-shadow`。
2. **数字即仪表** —— 一切数字走 Geist Mono + `tabular-nums` + `"tnum","zero"`，
   数字列右对齐，成列的数字必须上下对齐。
3. **唯一强调色** —— 琥珀橙只花在三处：当前导航项、主操作按钮、最重要的一组数据
   （`available` 徽章）。其余一切是墨色的不同透明度。

### token 分工（这是改版成本低的原因）

组件层几乎不用动，因为语义类名（`text-ink` / `border-line` / `bg-surface-*`）
早已覆盖 90% 的用法，换肤只需重定义 token：

| token | 含义 |
| --- | --- |
| `--accent` | **墨色**。active / hover / focus 这类高频状态，不消耗强调色；深色下自然反转成象牙墨 |
| `--action` | **琥珀橙**。全站唯一彩色 |
| `--accent-soft` | 琥珀浅底，给当前导航项 |

`.dark` 块只重定义原生 token（`--bg` / `--panel` / `--ink` / `--line` / `--amber` …），
语义层与几十个旧别名都是 `var()` 引用，会自动跟随。

### 字体

自托管 Geist Variable + Geist Mono Variable（100–900 可变字重，latin 子集，
`public/fonts/geist-*.woff2`，各 ~25KB），中文回落苹方 / 微软雅黑。
**这套语言没有衬线角色** —— `--font-serif` 等别名全部指向 sans。
开 `"cv11","ss01"`，数字额外开 `"tnum","zero"`。

### 改版时踩到的四个坑

1. **token 格式从 oklch 换成 RGB 三元组后，`oklch(var(--x))` 的引用会静默失效**。
   除 `index.css` 外还有 12 处：`DomainDrawer.tsx` 9 处、`valuation.css` 3 处。
2. **`@layer` 之外的规则优先级高于 `@layer base` 内的**。文件末尾那组全局滚动条定义
   会压掉 base 层里写的同名规则 —— 滚动条、`.skeleton` 这些都在末尾的全局区，改那里。
3. **`tailwind.config.js` 里的 `keyframes` 只在对应 `animate-*` 工具类被用到时才输出**。
   CSS 里用原生 `animation: enter-up …` 引用时，必须在 CSS 里自己写 `@keyframes`，
   否则动画名不存在、静默不动。
4. **`@layer components` 里定义但没被任何 tsx 引用的类会被 purge 掉**。
   `.chip` / `.live-dot` / `.tag` 当时都被摇掉了 —— 光定义不接线等于没做。

> 另外修掉一个**改版前就存在**的缺陷：`opacity` 是 `theme` 顶级覆盖（不是 `extend`），
> 白名单外的档位静默失效，而代码里一直在用 `/35`、`/55`、`/70` 和 `opacity-70`。已补齐。

### 组件落点

| 规范组件 | 落在哪 |
| --- | --- |
| 分段控件（滑动药丸） | `Layout.tsx` 的 `SegmentedControl`：thumb 是绝对定位元素，用 `useLayoutEffect` + `ResizeObserver` 测量位置，切换时滑动而非闪烁 |
| 筛选 chip | `Domains.tsx` 的 `filterChips` → `.chip .chip-active` |
| 状态标签 | `ui.tsx` 的 `StatusBadge` → `.tag`；`format.ts` 的 `STATUS_CLASSES` 改成「1px 同色描边」 |
| 实时状态点 | `Providers.tsx` 的 `STATE_CLASS`：healthy 用会呼吸的 `.live-dot`，其余用静态 `.state-dot` —— 离线的东西不该有心跳 |
| Logo | `Logo.tsx` 是**准星**内联 SVG（外圈扫描范围 + 四向刻度 + 中心锁定点，右上象限填强调色），悬停 -8°。不再套「圆角方块」 |
| 氛围层 | `AuroraBackground.tsx`：WebGL 极光，40% 分辨率、30fps 封顶、最终只取 8.5% 混合；颜色读 `--fx-*` token，主题切换由 MutationObserver 重新取色；WebGL 不可用 / 着色器编译失败 / `prefers-reduced-motion` 都优雅降级 |

面板半透明（`--panel` 82%）+ `backdrop-blur`，侧栏与顶栏 80% + blur，让氛围光从面板后面透上来。
侧栏宽 **198px**（原 248px），因此导航项的副标题只留在 `title` 提示里。

## 性能（2026-08-17 修复，两个独立根因）

现象是「静态资源秒开、但任何要读域名的接口都要好几秒」。查了两层，两个问题都真实存在且互相叠加。

### 根因二：列表查询退化成全表扫描（migration 020）

`internal/p1/service.go` 的 `loadDomains` 是域名列表页的主查询，它用
`lower(x.domain) = lower(d.name)` 关联三张表。**普通索引对表达式无效**，
`EXPLAIN QUERY PLAN` 显示 `SCAN d` + `SCAN r` + `SCAN v` 三个全表扫描，
外加一个逐行执行的相关子查询；而且这条查询没有 LIMIT，每次都拉全部 542 行。

修复：migration `020_lower_domain_join_indexes` 建三个表达式索引
（`domain_results` / `ai_domain_valuations` / `ai_domain_valuations_v2`）。
查询计划全部变成 `SEARCH USING INDEX`：

| | 实测（542 域名） |
| --- | --- |
| 加索引前 | 0.81–0.82s |
| 加索引后 | < 10ms |

行数与非空状态数校验一致（542/542），语义没有变化。

> 如果以后把 join 条件里的 `lower()` 去掉（改成列上存归一化后的域名），索引可以更简单；
> 现在这三个索引必须与 SQL 里的表达式**逐字一致**，否则又会退回全表扫描。

### 根因一：SQLite 连接池

根因不在查询本身（`select count(*) from domains` 裸跑 5ms），而在 `internal/storage/sqlite/db.go`：

- 数据库跑在默认的 rollback journal 模式，没开 WAL —— 每次写都要 fsync journal，
  在 NAS 的高 IO 负载下单次写可能几十毫秒。
- `SetMaxOpenConns(1)` 让整个进程只有一条连接。调度器 4 个 worker、每个域名 4–5 次写，
  前台请求只能排在这串写入后面。

修复：DSN 固化 `journal_mode(WAL)` + `synchronous(NORMAL)`，`busy_timeout` 提到 15s，
连接数放开到 4（WAL 下读不阻塞写；SQLite 仍是单写者，写冲突由 busy_timeout 兜底）。

实测 `/health`（做 2 次 DB 操作，是个合适的代理指标）：

| | 服务端本地 | 经 Tailscale 端到端 |
| --- | --- | --- |
| 修复前 | 0.77–3.58s，抖动大 | 6981–15144ms |
| 修复后 | 0.015–0.020s | 52–209ms |

注意：切 WAL 后数据目录会多出 `domainhunter.db-wal` / `-shm` 两个文件，
**用 `cp` 冷备份必须三个文件一起拷，或者停容器后再拷**（`internal/storage/sqlite/backup.go` 已考虑这点）。
现在日常备份走 `backup-db.sh` 的 `VACUUM INTO`，没有这个问题 —— 见「备份」。

## 「需要复核」筛选（2026-08-17 修复）

概览的「需要复核」卡片以前链到 `/domains?statuses=error,unknown,skipped`，但计数走的是
`domain.BuildReviewState(...).Required`（**证据维度**），而链接筛的是域名生命周期状态。
需复核的域名状态通常是 `grace` / `registered` / `pending_delete`，两者永远对不上 ——
卡片显示 3 个、点进去 0 条。

修复是补上缺失的筛选维度，而不是改链接去凑状态：

- `service.ListFilter` 加 `Review bool`；`applyFilter` 里跳过 `Review == nil || !Required`
  （`applyReview` 在 `List` 中已对全量算过，直接用即可）。
- `parseListFilter` 解析 `review=1`。
- 前端 `Overview.tsx` 链到 `/domains?review=1`，`Domains.tsx` 识别该参数并显示「需要复核」筛选 chip。

验证：概览计数 3 == `?review=1` 的 `total_filtered` 3（sumeru.org / 888888.org / catch-all.org，
状态均为 `grace`），旧链接仍然是 0 —— 正好复现了原来的错配。

> 同类风险：凡是「按证据/派生属性统计，却用状态参数去跳转」的入口都要检查。
> 目前 `/domains?status=available`（立即行动）与 `/domains?sort=expiry`（续费风险）
> 语义上仍是近似的，只是恰好能筛出结果。

## 外部查询源

`who-dat`(59090) 是主力，实测 p95 5ms，绝大部分查询由它接掉；其余都是兜底路径，很少被调用。

**2026-09-18 修掉 `vercel_who_dat`**：原默认值 `https://rdap.re` 已经变成域名停放页
（HTTP 200 但返回 HTML 跳 `/lander`），所以该 provider 长期报
`解析 who-dat 响应失败: invalid character '<'`。现改指 `https://whox.vercel.app/`。

> **坑**：`internal/query/providers/whodat/whodat.go` 的 `NewVercel()` 把 `rdap.re`
> **硬编码成空值时的回填**，因此清空环境变量或删掉 compose 里那行都无效，
> 必须显式指向一个可用端点。末尾的 `/` 会被 `New()` 的 `TrimRight` 去掉。

同期核实 `whois_ls` 的 HTTP 500 是外部临时故障，已自行恢复，无需处理。

## 域名管理（增删）

`domains` 表装的是**观察／待抢注目标，不是自有域名**。2026-09-18 按用户的 932 个已拥有域名
做过一次清理，交集只有 12 个（`573 → 561`）。

**删除必须走 API，不要直接写 SQL**：

```bash
# 登录拿 session cookie（POST /api/login 有 rate limit）
curl -c /tmp/c -X POST http://127.0.0.1:22334/api/login   -H 'Content-Type: application/json' -d '{"username":"...","password":"..."}'
# 批量删除
curl -b /tmp/c -X POST http://127.0.0.1:22334/api/v2/domains/batch-delete   -H 'Content-Type: application/json' -d '{"domains":["a.com","b.net"]}'   # -> {"deleted":2}
```

`DomainRepo.DeleteMany` 在**单事务里级联清理 4 张关联表**：`domain_results`、
`notification_history`、`domain_observations`、`query_attempts`。
`domains` 表**没有外键约束**，关联全靠 domain 文本字段 —— 所以裸 `DELETE FROM domains`
会在这 4 张表里留下孤儿数据。

> **中文域名**：`domain.Normalize()` 只做 `ToLower` + 去尾点，**不做 IDNA**，
> 入库前域名已转 punycode。比对时要自己 `encode('idna')`，
> 否则中文域名永远匹配不上（库里现有 `xn--com.com`=㯘.com、`xn--xxx.com`=燋.com）。

## 认证

用户名／bcrypt 哈希存在 `app_settings`（`server_username` / `server_password_hash`）。
API 走 session cookie 或 Bearer token（`withAuthScope`），匿名访问返回 401。

**2026-09-18 已清空遗留的明文密码** `app_settings.server_password`。查证依据：

- `internal/auth/auth.go:108` `verifyPassword()` 里 `if hash != "" { return bcrypt.Compare(...) }`
  先行返回，**明文分支在 hash 存在时永远到不了**。
- 明文只另外参与 `RequireAuth()`(auth.go:220，OR 的另一边，hash 非空已为 true) 和
  `UsesLegacyPassword()`(auth.go:227，hash 非空已为 false，仅用于返回展示字段 `password_hashed`)。
- 设置类 API 实测用 `*_set` 布尔模式（`smtp.password_set` 等），**不回显**任何明文值。

明文原本是 `auth.go:125` 注释所说的「首次明文登录自动升级为 hash 后，刻意留作当天回滚余量，
待下次改密码时由 `UpdatePassword(clearLegacy=true)` 清空」，因一直没改密码而留存。
清空后实测：原密码登录 200、错误密码 401、匿名 401，行为零变化。

> 同表的 `smtp_pass`（邮箱授权码）仍是明文，**不能清空** —— 应用必须读它才能发通知邮件，属设计使然。

## 已知约束

- 当前生产在 NAS（x86_64）。**若哪天要迁回树莓派**：Pi 是 ARM64，不能直接复用现有 x86_64 镜像，需在 Pi 上重新构建或确认多架构支持。
- `.im` 官方公开数据通常没有注册日期，不能通过到期日推断宽限期。
- WHOIS/RDAP 超时、空响应、保留和策略拒绝必须保持 `error`/`unknown`，不能转为 `available`。
- Browser/截图验证取决于当前运行环境；没有可用浏览器时，只能报告构建和 API/容器证据。
