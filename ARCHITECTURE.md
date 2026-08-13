# DomainHunter 架构

DomainHunter 是**单体 Go 应用 + SQLite + 单容器**。它的目标运行环境是树莓派、
Synology NAS 和普通 Linux VPS，因此不引入 Redis / Kafka / PostgreSQL /
Kubernetes 这类外部依赖。

## 目录结构

```
cmd/domainhunter/        进程入口：按顺序组装依赖，负责优雅关闭
internal/
├── domain/              领域模型：状态枚举、Domain/Info、Observation、Evidence
├── registry/            TLD → WHOIS/RDAP 端点映射 + WHOIS 文本识别词表
├── query/               查询引擎
│   ├── provider.go        Provider 接口与注册表
│   ├── policy.go          按 TLD 的查询顺序与 available 采信策略
│   ├── engine.go          执行计划、合成结论、产出证据
│   ├── result.go          统一的 QueryResult
│   ├── errors.go          带分类的查询错误
│   ├── health.go          Provider 健康度滑动窗口
│   ├── limiter.go         provider:tld 维度的限速（默认关闭）
│   ├── detect/            跨 Provider 共用的文本语义判定
│   ├── envcfg/            DOMAINHUNTER_* 环境变量读取
│   └── providers/         rdap · whois · whoisls · fallback
├── scheduler/           调度循环 + 三级优先级队列 + worker pool
├── service/             业务服务：Query / Domain / Monitor / Settings / Overview
├── repository/          存储接口
├── storage/sqlite/      SQLite 实现：迁移、备份、四个仓储
├── notification/        通知管理器、聚合器、email、telegram
├── auth/                会话与密码（bcrypt）
├── config/              应用配置（存于 app_settings）
├── httpapi/             路由、中间件、handler
├── httpx/               全局复用的 HTTP 客户端与代理拨号
└── logger/              带统一字段的结构化日志
web/                     React + TypeScript + Vite + Tailwind 管理端
```

依赖方向始终由外向内：`httpapi → service → repository/query → domain`。
`domain` 不依赖任何其他内部包。

## 运行时数据流

```
                   ┌──────────────────────────────┐
                   │            SQLite            │
                   │    domains.next_check_at     │
                   └───────────────┬──────────────┘
                                   │ 每 5s 扫描到期域名
                                   ▼
                          ┌────────────────┐
                          │   Scheduler    │
                          └────────┬───────┘
                                   │ Offer(Task)
                                   ▼
                 ┌──────────────────────────────────┐
                 │  优先级队列  manual > retry > 定时  │
                 └────────────────┬─────────────────┘
                                  │ Take
                                  ▼
                     ┌────────────────────────┐
                     │  Worker Pool (N 个)     │
                     └────────────┬───────────┘
                                  │ Execute(task)
                                  ▼
                     ┌────────────────────────┐
                     │      QueryService      │  ← 唯一的查询执行入口
                     └────────────┬───────────┘
                                  │
        ┌─────────────────────────┼──────────────────────────┐
        ▼                         ▼                          ▼
┌───────────────┐        ┌────────────────┐         ┌────────────────┐
│  QueryEngine  │        │  Repositories  │         │  Notification  │
│  + Policy     │        │  结果/观测/尝试  │         │  聚合 → 渠道    │
└───────┬───────┘        └────────────────┘         └────────────────┘
        │ 按计划依次调用
        ▼
┌──────────────────────────────────────────────┐
│ whois_ls → fallback → rdap → whois           │
│ 每一步产出 Result + Evidence                   │
└──────────────────────────────────────────────┘
```

HTTP 请求走另一条路径，但落到同一批 service：

```
浏览器 → httpapi(路由 + 认证 + CSRF + 限流) → service → repository / scheduler
```

手动"立即检查"不会另开一条查询实现：它把任务以 `manual` 优先级放进同一个队列，
等待同一个 `QueryService.Execute` 返回结果。

## 查询引擎

`Provider` 接口：

```go
type Provider interface {
    Name() string
    Supports(ctx context.Context, req Request) bool
    Query(ctx context.Context, req Request) Result
}
```

- `rdap` / `whois` 是通用源，`Supports` 恒为 true；某个后缀没有端点时在
  `Query` 内返回 `skipped`，这样详情页仍能看到"为什么没有这个源的结果"。
- `whois_ls` / `fallback` 是**按后缀显式启用**的专用源，未启用时 `Supports`
  返回 false，完全不参与查询。

`Policy.Plan()` 产出有序的 `Step`，每步带一个 `AvailableMode`：

| 模式 | 含义 |
| --- | --- |
| `trust` | 直接采信该源的 available 结论 |
| `confirm` | 需要另一个源同样报告 available 才采信 |
| `distrust` | 该源的 available 一律不采信，降级为 unknown |

默认（无配置）行为，与重构前逐条一致：

- 后缀没有启用专用源 → 全部 `trust`
- 后缀启用了专用源 → 专用源 `trust`，`rdap` / `whois` 为 `distrust`

`Engine.Query()` 的决策顺序：

1. 明确的非 available 结论 → 立即返回
2. available 且该步是 `trust` → 立即返回
3. available 且需要验证 → 挂起，继续找印证
4. 全部走完后：专用源的错误 > 未获印证的 available 降级为 unknown >
   最后一个 unknown > 全部 skipped > 错误

**永远不会出现的情况**：超时、连接失败、HTTP 404 语义不明、注册局策略文本、
空 RDAP 对象被当成 available。

## 调度

`domains` 表新增 `next_check_at` / `priority` / `retry_count`。调度器每
`PollInterval`（默认 5s）扫描一次：

```sql
SELECT ... FROM domains
WHERE enabled = 1 AND (next_check_at IS NULL OR next_check_at <= ?)
ORDER BY COALESCE(priority,0) DESC, next_check_at ASC
LIMIT ?
```

队列对同一域名去重，worker 数量等于设置里的"并发限制"，可在运行时热调整。
下次检查时间由 `service.NextInterval` 计算：

| 状态 | 间隔 |
| --- | --- |
| 正常状态（registered / available / grace / …） | 设置里的检查间隔 |
| error（第 1 次） | 5 分钟 |
| error（第 2 次） | 15 分钟 |
| error（第 3 次及以后） | 1 小时 |
| skipped | 24 小时 |

## 存储

| 表 | 用途 |
| --- | --- |
| `app_settings` | 全部配置（含密码哈希、会话密钥、查询策略） |
| `domains` | 监控列表 + 调度状态 |
| `domain_results` | **当前状态快照**，首页与列表直接读它 |
| `domain_observations` | 每次查询的历史观测，`changed` 标记状态变化 |
| `query_attempts` | 每个 Provider 的单次尝试（状态、耗时、错误、原文） |
| `notification_history` | 通知去重 |
| `saved_views` | 版本化高级筛选条件与共享视图 |
| `ai_provider_settings` | DeepSeek 配置与加密 Key 密文（不存明文） |
| `ai_jobs` | 可租约恢复、重试、并发和去重的持久化 AI Job；不设置每日估价额度 |
| `ai_domain_valuations` | 严格 Schema 校验后的研究性估价与 TTL |
| `automation_rules` | 触发器、条件、安全动作与防护栏 |
| `automation_runs` | Dry-run/执行审计与 `(rule,event,domain)` 幂等 |
| `automation_cursors` | 新增域名、观测与每日到期扫描的事件游标，避免重放旧数据 |
| `bulk_action_audits` | 批量变更/AI 入队的匹配数、任务数与结果审计 |
| `schema_migrations` | 已应用的迁移版本 |

迁移在启动时执行，有待执行迁移时先 `VACUUM INTO` 生成一份一致备份
（`data/backups/<库名>-YYYYMMDD-HHMMSS-*.db`，只保留最近 5 份），备份失败则中止升级。

历史表有保留上限（默认 180 天 / 每域名 200 条 / 原始报文仅在状态变化时保存且
最多 16KB），保证长期运行不会把数据库撑爆。

## P1 数据流

```
URL 条件树 / 保存视图 ──→ Advanced Filter ──→ 域名列表与批量预览
                                               │
                                               ├─ 安全批量写入（单事务）
                                               └─ AI Job Repository
                                                     │ 租约 / 重试 / 并发
                                                     ▼
                                            DeepSeek Provider
                                                     │ 严格 JSON Schema
                                                     ▼
                                         ai_domain_valuations（TTL）

新增域名 / 观测完成 / 状态变化 / 异常恢复 / 临近到期
        │（持久化游标，30s 扫描）
        ▼
Automation Rule → 条件匹配 → Dry-run / 白名单动作 → automation_runs
                                             │
                                             └─批量变更或 AI 入队 → bulk_action_audits
```

AI 是研究、排序和解释层，不能写入查询引擎的 `available` 结论；自动化也不拥有
任意网络调用或高风险外部操作权限。

## 通知

```
QueryService 检测到状态变化
        │  （首查不发、error→X 不发、证据不足的 available 不发）
        ▼
notification.Manager.Submit
        ▼
   Aggregator（10s 内合并，或 8s 无新查询时提前发出）
        ▼
   Manager.dispatch → 各 Notifier.Send(ctx, Event)
                          ├── email
                          └── telegram
```

新增渠道（Webhook / Bark / Discord / Slack）只需实现 `Notifier` 接口并
`Manager.Register`，聚合、去重与格式化都不用改。

## 前端

React 19 之前的稳定组合：React 18 + TypeScript + Vite + Tailwind v3，
零运行时 UI 框架依赖（组件手写在 `web/src/components/ui.tsx`）。

```
web/src → vite build → web/dist → go:embed → 单个二进制
```

`web/dist` 随仓库提交，因此 `go build` 与 GoReleaser 在没有 Node 的环境里
也能产出完整可用的二进制；Docker 镜像同样不需要 Node 阶段。修改前端后必须
重新 `npm run build` 并提交 `dist`（CI 会校验两者一致）。

## 关闭顺序

```
SIGINT/SIGTERM
   → HTTP Server.Shutdown（停止接收新请求）
   → MonitorService.Stop（取消 ctx，等待正在执行的查询结束）
   → Notification.Stop（聚合器 flush，队列排空）
   → Authenticator.Stop
   → DB.Close
```
