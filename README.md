# DomainHunter

<p align="center">
  <a href="https://github.com/NextCandy/DomainHunter">
    <img src="https://raw.githubusercontent.com/NextCandy/DomainHunter/main/web/public/DomainHunter.svg" alt="DomainHunter logo" width="132" />
  </a>
</p>

<h1 align="center">DomainHunter</h1>

<p align="center">
  RDAP-first 域名监控与研究工作台
</p>

<p align="center">
  保守判断可注册状态，保留可追溯证据，提供抢注窗口、通知中心与研究性 AI 域名估价。
</p>

<p align="center">
  <a href="https://github.com/NextCandy/DomainHunter/actions/workflows/ci.yml"><img src="https://github.com/NextCandy/DomainHunter/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/NextCandy/DomainHunter/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-0f766e.svg" alt="MIT License" /></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.24%2B-00ADD8.svg" alt="Go 1.24+" /></a>
  <a href="https://github.com/NextCandy/DomainHunter"><img src="https://img.shields.io/badge/stack-Go%20%2B%20React%20%2B%20SQLite-4a4636.svg" alt="Go React SQLite" /></a>
</p>

> 当前版本基线：`v2.10.0` · Seline 暖纸张工作台 · DeepSeek 官方 OpenAI-compatible · `.im` 稳定生命周期判断

## 产品定位

DomainHunter 是一个面向长期运行的 Go 域名监控器。它把多个查询源的结果合并成保守的结构化结论，并把每个 Provider 的状态、耗时、错误和原始证据保存到 SQLite，方便回答“为什么现在是这个状态”。

核心原则：**无法确认就不报告可注册**。

- 只有明确的未注册信号才会进入“可注册”。超时、空响应、连接失败、保留域名和策略拒绝不会被推断为可注册。
- 查询源冲突、低可信度和证据过期会进入复核，不会污染通知和抢注排序。
- 状态历史完整保留：`registered → grace → redemption → pending_delete → available`。
- AI 只生成研究性评分、人民币价格区间和用途分析，不改变域名状态、可信度或通知结论。
- AI 输入只包含最小化结构化事实，不发送 WHOIS/RDAP 原文、联系人、备注、密码或 Token。

## 功能总览

| 模块 | 能力 |
| --- | --- |
| 今日工作台 | 总量、状态分布、行动队列、趋势、查询源健康和最近变化 |
| 域名资产 | 搜索、状态/TLD/注册商筛选、排序、分页、批量检查、收藏、文件夹、导入导出 |
| 抢注看板 | 只显示可注册、待删除、赎回期、已过期和宽限期域名，并按紧迫度排序 |
| 查询历史 | 全局状态变化、单域名时间线、每个查询源的历史尝试 |
| 查询源 | Provider 健康度、IANA RDAP bootstrap、查询策略和限速 |
| 通知中心 | 邮件、Telegram、Bark、飞书机器人、自定义 Webhook；每个渠道可单独测试 |
| AI 与自动化 | DeepSeek 档案、模型、无限估价队列、缓存、AI Job、Dry-run 自动化规则 |
| 系统设置 | 监控参数、历史保留、账号、API Token、数据库备份和维护 |

域名详情以右侧抽屉呈现，包含：概览、查询证据、状态时间线、原始报文和研究性 AI 鉴定报告。

## 视觉系统

前端采用 React 18 + TypeScript + Vite + Tailwind，按照 `DESIGN (1).md` 重做为安静的分析工作台：

- `#fafaf9` 暖石色画布，白色平面卡片，`#e8e6e5` 细边框。
- `#3ba6f1` 单一青蓝信号，只用于主要操作、当前导航和可注册重点。
- Roobert 风格展示字体 + Inter UI 字体 + 等宽元数据字体。
- 10px 卡片圆角、9999px 操作胶囊、宽松内边距和低对比结构线。
- 桌面端左侧工作区导航；移动端顶部完整菜单和底部五项导航。
- 表格在窄屏自动切换为可读卡片，不把数据压缩成不可用的小表格。

Logo 资源位于 [`web/public/DomainHunter.svg`](web/public/DomainHunter.svg)，同时提供 [`web/public/DomainHunter.png`](web/public/DomainHunter.png) 作为应用图标。

## 快速开始

需要 Go 1.24+ 或 Docker：

```bash
git clone https://github.com/NextCandy/DomainHunter.git
cd DomainHunter
docker compose up -d --build
curl -f http://127.0.0.1:8080/health
```

默认端口为 `8080`。首次启动会创建 `data/domainhunter.db`；默认账号为 `domainhunter`，请登录后立即修改密码。

本地开发：

```bash
go test ./...
go build -o domainhunter ./cmd/domainhunter

cd web
npm ci
npm run dev
```

发布前构建前端嵌入资源：

```bash
cd web
npm run typecheck
npm run lint
npm run build
cd ..
go test ./...
```

`web/dist` 会随仓库提交，并由 `go:embed` 嵌入最终二进制；生产容器不需要 Node.js。

## 查询链路

默认查询链路按后缀策略执行：

```text
who-dat → WHOIS.LS / 本地结构化 fallback → whois-domain-lookup
        → rdap.re → RDAP → rdap.org → WHOIS → 可选 Spaceship → AI 研究兜底
```

实际顺序由管理端“查询源”页面或 `app_settings.query_policy` 控制。没有可用查询源时显示 `skipped`，不会伪装成 `available`。

### `.im` 特殊规则

- `.im` 公开结果通常不提供创建日期，创建日期保持为空，不猜测。
- 只有到期日而没有明确生命周期字段时，按“已注册、阶段无法确认”处理，避免宽限期/已注册来回重复提醒。
- `.im` 可注册必须有至少两个独立来源确认。
- 如果基础来源无法确认且配置了 Spaceship API，最后通过 `GET /api/v1/domains/{domain}/available` 补充可用性事实。
- Spaceship 只补充可用/不可用，不提供 `.im` 注册日期或生命周期阶段。

### `.do` 特殊规则

结构化 `registered`、`reserved`、`unknown` 字段优先于 WHOIS 文本中的隐私标记，例如 `Registry Policy` 不会被误判为保留域名。

## AI 域名估价

默认档案：

| 字段 | 值 |
| --- | --- |
| Provider | OpenAI Compatible |
| Base URL | `https://api.deepseek.com` |
| Model | `deepseek-v4-flash` |

API Key 只通过 `DOMAINHUNTER_AI_API_KEY` 或管理端安全配置注入，不写入 GitHub。AI 报告格式为：

```text
域名，
评分：X 分，
价格评估：XX-XXX 元，
核心分析：语义、记忆点、后缀适用性、前缀习惯、用途、市场需求与溢价空间。
```

AI 是研究和排序工具，不是注册状态判定器、成交保证或投资建议。
应用不设置每日 AI 估价额度；并发、超时、缓存和上游 Provider 限流仍然生效。

## Docker 与树莓派部署

本地 Compose：

```bash
docker compose config -q
docker compose up -d --build
curl -f http://127.0.0.1:8080/health
```

树莓派配置见 [`deploy/raspberry-pi.compose.yaml`](deploy/raspberry-pi.compose.yaml)。生产默认：

- DomainHunter 容器：`DomainHunter`
- 项目目录：`/opt/docker-migrated/domainhunter`
- 对外端口：`22334 → 8080`
- SQLite：`/opt/docker-migrated/domainhunter/domainhunter.db`
- who-dat：`59090`
- whois-domain-lookup：`12121`
- CPU shares：`512`，不设置内存上限

生产切换前必须执行：

```bash
docker compose -f /opt/docker-migrated/domainhunter/compose.yaml config -q
docker image inspect <new-image>
sqlite3 /opt/docker-migrated/domainhunter/domainhunter.db 'PRAGMA integrity_check;'
curl -f http://127.0.0.1:22334/health
```

升级与回滚步骤见 [`MIGRATION.md`](MIGRATION.md)，当前版本交接证据见 [`HANDOFF.md`](HANDOFF.md)。

## 数据与备份

```text
data/
├── domainhunter.db
└── backups/
    └── domainhunter-YYYYMMDD-HHMMSS-*.db
```

迁移和手工备份使用 SQLite `VACUUM INTO`，不会直接复制运行中的 WAL 文件。默认只保留最近 5 份自动备份；清理只匹配 `domainhunter-*.db`。

## 环境变量

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `DOMAINHUNTER_DATA_DIR` | `data` | 数据目录 |
| `DOMAINHUNTER_DB_FILE` | `domainhunter.db` | 数据库文件名或路径 |
| `DOMAINHUNTER_PORT` | `8080` | HTTP 监听端口 |
| `DOMAINHUNTER_WHO_DAT_URL` | 空 | who-dat 服务地址 |
| `DOMAINHUNTER_WHO_DAT_API_KEY` | 空 | who-dat Bearer Key |
| `DOMAINHUNTER_WHOIS_FALLBACK_URL` | 空 | 结构化备用服务 |
| `DOMAINHUNTER_WHOIS_FALLBACK_TLDS` | `im,do` | 备用服务后缀 |
| `DOMAINHUNTER_WHOIS_LS_URL` | 空 | WHOIS.LS JSON 网关 |
| `DOMAINHUNTER_WHOIS_LS_TLDS` | `im` | WHOIS.LS 后缀 |
| `DOMAINHUNTER_SPACESHIP_API_URL` | `https://spaceship.dev/api/v1` | Spaceship API 根地址 |
| `DOMAINHUNTER_SPACESHIP_API_KEY` | 空 | Spaceship API Key |
| `DOMAINHUNTER_SPACESHIP_API_SECRET` | 空 | Spaceship API Secret |
| `DOMAINHUNTER_AI_API_KEY` | 空 | DeepSeek 运行时 Key |
| `DOMAINHUNTER_SECRET_KEY` | 空 | 加密保存 AI Key 的主密钥 |
| `DOMAINHUNTER_AI_ALLOWED_HOSTS` | `api.deepseek.com` | AI 出站主机 allowlist |
| `DOMAINHUNTER_ALLOW_INSECURE_AI_BASE_URL` | `false` | 是否允许本地 HTTP AI 端点 |
| `DOMAINHUNTER_COOKIE_SECURE` | `auto` | Cookie Secure 策略 |
| `DOMAINHUNTER_CORS_ORIGINS` | 空 | CORS 来源列表 |
| `DOMAINHUNTER_CSRF_ENABLED` | `true` | CSRF 校验 |
| `DOMAINHUNTER_LOG_LEVEL` | `info` | 日志级别 |

密钥只通过环境变量或受保护的管理端配置输入，禁止提交到仓库。

## 项目结构

```text
cmd/domainhunter/        Go 进程入口
internal/domain/         状态、证据和领域模型
internal/query/          查询引擎、策略、Provider 和限速
internal/service/        域名、监控、通知、设置和概览服务
internal/storage/sqlite/ SQLite 迁移、备份和仓储
internal/httpapi/        API 路由、认证、CSRF 和 handler
internal/ai/             严格研究性 AI 估价
internal/p1/             AI 档案、Job 和自动化
web/src/                 React 页面与组件
web/dist/                go:embed 生产静态资源
deploy/                  树莓派 Compose 配置
```

## 验证

```bash
go test ./...
go vet ./...
cd web && npm run typecheck && npm run lint && npm run build
```

CI 会验证 Go 测试、前端 lint/build、`web/dist` 与源码一致性以及 Docker 构建。部署后还需验证 `/health`、容器健康状态、SQLite integrity、重启次数、OOM 状态和查询源连通性。

## 许可证

MIT License，详见 [`LICENSE`](LICENSE)。
