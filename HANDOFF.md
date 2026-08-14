# DomainHunter 当前交接说明

## 当前范围

本仓库是独立的 DomainHunter Go/React 项目，运行形态为单体 Go 应用、SQLite 和单容器。前端构建产物位于 `web/dist`，由 `go:embed` 编入二进制。

## 生产部署

| 项目 | 当前值 |
| --- | --- |
| 实例目录 | `/opt/docker-migrated/domainhunter` |
| Compose 项目 | `domainhunter` |
| 容器 | `DomainHunter` |
| 服务端口 | `22334 → 8080` |
| 数据库 | `/opt/docker-migrated/domainhunter/domainhunter.db` |
| who-dat | `59090 → 8080` |
| whois-domain-lookup | `12121 → 80` |
| CPU shares | `512` |
| 内存上限 | 不设置 |

DomainHunter 镜像升级不会重建两个独立查询服务。生产切换前备份 Compose 和 SQLite，并先通过 `docker compose config -q`。

## 当前产品能力

- RDAP-first，多 Provider 证据合并和保守可注册判定。
- `.im` 只在至少两个来源确认时判可注册；到期日可有、注册日期保持为空。
- 可选 Spaceship API 只补充 `.im` 的可用/不可用事实。
- who-dat 与结构化 whois-domain-lookup 可作为树莓派本地查询源。
- DeepSeek 官方 OpenAI-compatible 默认档案：`https://api.deepseek.com` + `deepseek-v4-flash`。
- AI 只输出评分、人民币价格区间和核心分析，不改变域名生命周期结论。
- 暖纸张分析工作台 UI，桌面侧栏与移动底部导航，表格窄屏转卡片。

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

## 部署门禁

```bash
docker build --build-arg VERSION=vX.Y.Z -t domainhunter:vX.Y.Z .
docker run --rm --name domainhunter-smoke domainhunter:vX.Y.Z ./domainhunter --help
docker compose -f /opt/docker-migrated/domainhunter/compose.yaml config -q
curl -f http://127.0.0.1:22334/health
```

部署后记录：镜像 ID、health、restart count、OOMKilled、SQLite integrity、域名数量、关键 API 响应和容器日志。

## 回滚

优先把 Compose 镜像改回部署前的 rollback tag，再只重建 DomainHunter。数据库恢复仅在确认数据或迁移异常时执行，恢复前必须停容器并再次核对备份的 `PRAGMA integrity_check`。

## 已知约束

- 树莓派是 ARM64，不能直接复用 x86_64 镜像；需在 Pi 构建或确认镜像多架构支持。
- `.im` 官方公开数据通常没有注册日期，不能通过到期日推断宽限期。
- WHOIS/RDAP 超时、空响应、保留和策略拒绝必须保持 `error`/`unknown`，不能转为 `available`。
- Browser/截图验证取决于当前运行环境；没有可用浏览器时，只能报告构建和 API/容器证据。
