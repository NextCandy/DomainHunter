# DomainHunter

DomainHunter 是一个面向长期监控的 Go 域名状态查询器。它保留了 Puff 的 SQLite 数据和 Web 管理界面，同时把查询链路重建为“RDAP 优先、WHOIS 兼容、无法确认就不报可注册”的安全模型。

项目名称为 DomainHunter；GitHub 仓库使用 `NextCandy/DomainHunter-go`，因为账号下已有 `NextCandy/dh`，GitHub 会将 `DomainHunter` 路径解析到那个无关项目。

## 设计目标

- 使用 IANA RDAP bootstrap 动态补充 TLD → RDAP 服务映射，并保留内置静态配置作为离线兜底。
- 只有收到明确的未注册信号才显示“可注册”；HTTP 404、空 RDAP 对象、超时、注册局策略拒绝和保留域名不会被推断为可注册。
- 没有可用 RDAP/WHOIS 查询源的后缀显示“已跳过”，不会把“无法查询”伪装成“可注册”。
- 仅对当前启用的域名读写查询结果。删除域名与查询完成并发时不会重新写回孤儿结果，统计和列表使用同一数据边界。
- 支持按 TLD 明确启用备用 WHOIS 服务；备用服务异常时保留未知/错误，不覆盖成可注册。
- `.im` 可通过 WHOIS.LS 获取公开的到期日期；由于该注册局公开响应不提供创建/注册日期，DomainHunter 会保留创建日期为空，不会猜测或填充伪造日期。
- 兼容原 Puff 的 `data/puff.db` 文件名和 `PUFF_WHOIS_FALLBACK_*` 环境变量，升级时无需重建域名列表。

RDAP 是 ICANN 推动的结构化 WHOIS 替代协议，服务发现使用 IANA 的官方 bootstrap 数据。实现思路参考了 [ICANN RDAP](https://www.icann.org/rdap/)、[IANA RDAP bootstrap](https://data.iana.org/rdap/dns.json)、[Domain Check](https://github.com/jedarden/domain-check) 和 [who-dat](https://github.com/lissy93/who-dat) 的 RDAP-first / WHOIS fallback 分层方式。

## 快速开始

需要 Go 1.24+ 或 Docker。

```bash
git clone https://github.com/NextCandy/DomainHunter-go.git
cd DomainHunter-go
docker compose up -d --build
```

默认端口为 `8080`。新安装登录后请立即修改初始账号密码。已有 Puff 安装只需把原 `data/puff.db` 挂载到 `/app/data/puff.db`，账号、域名列表和监控设置会继续使用数据库中的值。

本地编译运行：

```bash
go test ./...
go build -o domainhunter .
./domainhunter
```

## Docker Compose

仓库内的 `compose.yaml` 使用本地构建镜像，数据只写入 `./data`：

```bash
docker compose config -q
docker compose up -d --build
curl http://127.0.0.1:8080/health
```

备用查询服务是可选项。只对已经确认需要它的后缀配置，例如：

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

旧变量名 `PUFF_WHOIS_FALLBACK_URL`、`PUFF_WHOIS_FALLBACK_TLDS` 和 `PUFF_WHOIS_FALLBACK_TIMEOUT` 仍可使用，便于滚动升级旧部署。

WHOIS.LS 的接口文档见 [WHOIS.LS API](https://whois.ls/api)。它只作为明确配置的 `.im` 网络源使用；网络错误仍显示为错误/未知，不会推断域名可注册。

## 数据与升级

- 数据库文件固定为 `data/puff.db`，这是为了无损兼容现有 Puff 部署，并不影响项目名称已经更改为 DomainHunter。
- 升级前应备份数据库；Compose 更新只替换容器和程序，不删除数据目录。
- `GET /health` 用于检查服务、数据库、配置和通知组件。
- 查询状态包括：可注册、已注册、宽限期、赎回期、待删除、查询错误、未知状态和已跳过。

## 发布

GitHub Release 发布后，`.github/workflows/goreleaser.yml` 负责生成跨平台归档；`.github/workflows/docker.yml` 负责构建 `ghcr.io/nextcandy/domainhunter` 的 amd64/arm64 镜像。发布前至少执行：

```bash
gofmt -w $(find . -name '*.go' -not -path './vendor/*')
go test ./...
docker compose config -q
```

## License

MIT License，详见 [LICENSE](LICENSE)。
