# DomainHunter

DomainHunter 是一个面向**长期监控**的 Go 域名状态查询器。它保留了 Puff 的 SQLite
数据与 Web 管理界面，把查询链路建立在"RDAP 优先、WHOIS 兼容、**无法确认就不报告
可注册**"的安全模型上，并为每个结论保留可追溯的查询证据。

单体 Go 应用 + SQLite + 单容器，适合树莓派、Synology NAS 与普通 Linux VPS 自托管。

> 项目名称为 DomainHunter；GitHub 仓库使用 `NextCandy/DomainHunter-go`，因为账号下
> 已有 `NextCandy/dh`，GitHub 会把 `DomainHunter` 路径解析到那个无关项目。

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
- 兼容原 Puff 的 `data/puff.db` 与 `PUFF_*` 环境变量，升级无需重建任何数据。

## 快速开始

需要 Go 1.24+ 或 Docker。

```bash
git clone https://github.com/NextCandy/DomainHunter-go.git
cd DomainHunter-go
docker compose up -d --build
curl -f http://127.0.0.1:8080/health
```

默认端口 `8080`，默认账号 `domainhunter / domainhunter123`，**登录后请立即修改密码**。

已有 Puff / DomainHunter 安装只需把原 `data/puff.db` 挂到 `/app/data/puff.db`，
账号、域名列表与监控设置会继续使用数据库中的值，详见 [MIGRATION.md](MIGRATION.md)。

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
                     Repository  ←────────────────  SQLite (puff.db)
                                                          ↓
                                                   Notification
```

完整说明见 [ARCHITECTURE.md](ARCHITECTURE.md)。

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
├── puff.db                 主数据库（文件名保持不变以兼容既有部署）
└── backups/
    └── puff-*.db           迁移前自动生成的备份，只保留最近 5 份
```

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `DOMAINHUNTER_DATA_DIR` | `data` | 数据目录 |
| `DOMAINHUNTER_PORT` | 数据库中的 `server_port`（8080） | 监听端口 |
| `DOMAINHUNTER_RDAP_BOOTSTRAP_URL` | `https://data.iana.org/rdap/dns.json` | IANA bootstrap |
| `DOMAINHUNTER_WHOIS_FALLBACK_URL` | 空 | 本地结构化备用服务地址 |
| `DOMAINHUNTER_WHOIS_FALLBACK_TLDS` | `im,do` | 启用备用服务的后缀 |
| `DOMAINHUNTER_WHOIS_FALLBACK_TIMEOUT` | `20` | 秒 |
| `DOMAINHUNTER_WHOIS_LS_URL` | 空 | WHOIS.LS JSON 网关 |
| `DOMAINHUNTER_WHOIS_LS_TLDS` | `im` | 启用 WHOIS.LS 的后缀 |
| `DOMAINHUNTER_WHOIS_LS_TIMEOUT` | `20s` | 支持 `20` 或 `20s` |
| `DOMAINHUNTER_QUERY_POLICY_FILE` | 空 | 查询策略 JSON 文件（优先于数据库设置） |
| `DOMAINHUNTER_COOKIE_SECURE` | `auto` | `auto` / `true` / `false` |
| `DOMAINHUNTER_COOKIE_SAMESITE` | `lax` | `lax` / `strict` / `none` |
| `DOMAINHUNTER_CORS_ORIGINS` | 空 | 逗号分隔；为空表示仅同源 |
| `DOMAINHUNTER_CSRF_ENABLED` | `true` | 写操作的 CSRF 校验 |
| `DOMAINHUNTER_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `DOMAINHUNTER_LOG_FORMAT` | `text` | 设为 `json` 输出结构化日志 |

对应的 `PUFF_*` 旧变量名仍然有效（已废弃，但不会被删除）。

## 查询 Provider

| Provider | 类型 | 说明 |
| --- | --- | --- |
| `rdap` | 通用 | 结构化优先；只有合法的 RDAP 错误对象 + 明确的"不存在"语义才判可注册 |
| `whois` | 通用 | 注册局 43 端口；状态识别词表见 `internal/registry/detection_patterns.json` |
| `whois_ls` | 按后缀启用 | WHOIS.LS JSON 网关，部署上用于 `.im` |
| `fallback` | 按后缀启用 | 本地 whois-domain-lookup 结构化服务，部署上用于 `.im` / `.do` |

查询顺序与"可注册"的采信程度可以按 TLD 配置（管理端「查询源」页，或
`app_settings.query_policy`）：

```json
{
  "providers": { "whois_ls": { "enabled": true } },
  "tlds": {
    "im": { "providers": ["whois_ls", "rdap", "whois"], "validate_available": true },
    "do": { "providers": ["fallback", "rdap", "whois"], "validate_available": true }
  }
}
```

`validate_available` 取 `true`（= `"distrust"`，不单独采信）、`"confirm"`
（需要第二个来源印证）或 `"trust"`。**留空即保持默认行为**，无需任何配置。

### 关于 `.im` 与 `.do`

- `.im`：注册局公开响应**不提供创建日期**。DomainHunter 保留创建日期为空，
  绝不猜测或填充伪造日期；到期日通过 WHOIS.LS 获取。
- `.do`：响应会给每个隐私字段追加 `| Registry Policy`。这只是隐私标记，
  明确的 `registered=true` 优先，不会被误判为保留域名。

## 支持状态

`available` `registered` `grace` `redemption` `pending_delete` `expired`
`transfer_locked` `hold` `unknown` `error` `skipped`

## 通知

内置邮件与 Telegram。抑制规则：

- 首次查询不通知（避免初始化时刷屏）
- 从 `error` 恢复不通知
- 目标状态本身不需要通知（`unknown` / `error` / `skipped`）时不通知
- **可注册结论证据不足时不通知**

新增渠道只需实现 `notification.Notifier` 接口。

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
- 恢复：停容器 → 用备份覆盖 `data/puff.db` → 删除 `puff.db-wal` / `puff.db-shm`
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
| 数据库越来越大 | 「系统设置 → 历史数据保留」下调保留天数 / 每域名条数 / 原始报文策略 |

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
