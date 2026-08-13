# DomainHunter 升级与回滚

本文用于当前 DomainHunter 安装的版本升级、数据库备份、Compose 切换和回滚。

## 升级前检查

```bash
docker inspect DomainHunter --format '{{.Config.Image}}'
docker compose -f compose.yaml config -q
sqlite3 data/domainhunter.db 'PRAGMA integrity_check;'
sqlite3 data/domainhunter.db 'SELECT COUNT(*) FROM domains;'
```

记录当前镜像、域名数量和数据库完整性。生产环境只切换 DomainHunter 容器，不要重建 who-dat 或 whois-domain-lookup 依赖服务。

## 数据库备份

运行中的 SQLite 不应直接复制 `db`、`db-wal` 和 `db-shm` 文件。使用应用的 `VACUUM INTO` 或在停机后执行一致性备份：

```bash
sqlite3 data/domainhunter.db ".backup 'data/backups/domainhunter-manual-$(date +%Y%m%d-%H%M%S).db'"
sqlite3 data/backups/<verified-backup>.db 'PRAGMA integrity_check;'
```

管理端“系统设置 → 维护 → 立即备份数据库”会通过同一套安全备份逻辑生成 `domainhunter-*.db` 文件。

## 本地升级

```bash
npm --prefix web ci
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web run build
go test ./...
docker compose config -q
docker compose up -d --build
curl -f http://127.0.0.1:8080/health
```

迁移只追加表、索引和字段，启动时会在执行待迁移前自动生成备份。迁移失败会停止启动，不会继续修改数据库。

## 树莓派升级

树莓派为 ARM64，优先在目标机上构建镜像：

```bash
cd /opt/docker-migrated/domainhunter
cp compose.yaml compose.yaml.pre-upgrade-$(date +%Y%m%d-%H%M%S)
sqlite3 domainhunter.db ".backup 'backups/domainhunter-manual-$(date +%Y%m%d-%H%M%S).db'"
docker compose -f compose.yaml config -q
docker build --build-arg VERSION=vX.Y.Z -t domainhunter:vX.Y.Z .
docker tag domainhunter:vX.Y.Z domainhunter:rollback-pre-vX.Y.Z
# 将 compose.yaml 的 image 改为新版本后：
docker compose -f compose.yaml up -d --no-build --no-deps domainhunter
curl -f http://127.0.0.1:22334/health
```

切换后检查：

```bash
docker ps --filter name=DomainHunter
docker inspect DomainHunter --format '{{.RestartCount}} {{.State.OOMKilled}}'
sqlite3 domainhunter.db 'PRAGMA integrity_check; SELECT COUNT(*) FROM domains;'
docker logs --tail 100 DomainHunter
```

who-dat 和 whois-domain-lookup 的容器、端口和数据不属于 DomainHunter 镜像切换范围。

## 回滚程序

优先只回滚镜像，不回滚数据库：

```bash
docker tag domainhunter:rollback-pre-vX.Y.Z domainhunter:vX.Y.Z
# 把 compose.yaml 的 image 改回记录的镜像
docker compose -f compose.yaml up -d --no-build --no-deps domainhunter
curl -f http://127.0.0.1:22334/health
```

只有确认数据库结构或数据异常时才恢复数据库备份：

```bash
docker compose -f compose.yaml stop domainhunter
cp backups/<verified-backup>.db domainhunter.db
rm -f domainhunter.db-wal domainhunter.db-shm
sqlite3 domainhunter.db 'PRAGMA integrity_check;'
docker compose -f compose.yaml up -d --no-build --no-deps domainhunter
```

## 环境变量

所有部署变量使用 `DOMAINHUNTER_*` 前缀。关键变量包括：

- `DOMAINHUNTER_DB_FILE`
- `DOMAINHUNTER_WHO_DAT_URL`
- `DOMAINHUNTER_WHOIS_FALLBACK_URL`
- `DOMAINHUNTER_WHOIS_LS_URL`
- `DOMAINHUNTER_SPACESHIP_API_KEY` / `DOMAINHUNTER_SPACESHIP_API_SECRET`
- `DOMAINHUNTER_AI_API_KEY`
- `DOMAINHUNTER_SECRET_KEY`

密钥不写入 Compose、GitHub 或 README；生产通过实例环境文件或受保护的密钥管理注入。

## 验证清单

```text
[ ] 新旧镜像 ID 已记录
[ ] Compose config -q 通过
[ ] 数据库备份存在且 integrity_check=ok
[ ] /health 返回 status=ok
[ ] 容器 healthy、restart=0、OOMKilled=false
[ ] 域名数量与升级前一致
[ ] 登录、域名列表、详情和立即检查可用
[ ] .im 生命周期不重复切换提醒
[ ] who-dat 与 whois-domain-lookup 依赖服务未被重建
[ ] 日志没有 migration、fatal 或数据库锁错误
```
