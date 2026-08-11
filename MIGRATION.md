# 升级与回滚指南

本文覆盖三个阶段：

```
旧版 Puff  →  当前 DomainHunter(v1)  →  重构版 DomainHunter(v2)
```

## 结论先说

- **数据库文件路径不变**：仍然是 `data/puff.db`。不要重命名。
- **不需要重建域名列表、账号或通知设置**，全部沿用数据库里的值。
- **旧的 `PUFF_*` 环境变量继续有效**，可以先升级程序、再逐步改环境变量。
- 升级时会**自动生成一份数据库备份**再执行迁移；备份失败则中止升级。

---

## 1. 从旧版 Puff 升级到 DomainHunter

只需要把原来的 `puff.db` 挂载到容器的 `/app/data/puff.db`：

```yaml
volumes:
  - ./data:/app/data     # data/puff.db 就是原来的库
```

账号、域名列表、监控设置都存在 `app_settings` / `domains` 表里，程序启动后
直接读取。

## 2. 从 DomainHunter v1 升级到重构版 v2

### 2.1 数据库变化

v2 引入了 `schema_migrations` 表并按版本执行迁移：

| 版本 | 名称 | 内容 |
| --- | --- | --- |
| 001 | baseline | 既有的 4 张表（老库会被自动标记为"已应用"，不会重复建表） |
| 002 | domain_scheduling_columns | `domains` 新增 `priority` / `retry_count` / `next_check_at` / `favorite` / `note` / `tags` / `last_notified_status` 以及调度索引 |
| 003 | observation_history | 新增 `domain_observations` |
| 004 | query_attempts | 新增 `query_attempts` |
| 005 | auth_hardening | `app_settings` 预置 `server_password_hash` / `session_secret` 两个键 |

**全部是新增，没有任何列被重命名或删除**，`domains` / `domain_results` /
`notification_history` / `app_settings` 的原有列语义完全不变。

因此 **v1 的二进制仍然能读 v2 迁移后的数据库**：它只是看不到新表和新列。
这一点是回滚能够无损进行的前提。

### 2.2 自动备份

启动时如果检测到待执行的迁移：

1. 用 SQLite 的 `VACUUM INTO` 生成 `data/backups/puff-YYYYMMDD-HHMMSS-pre-migration.db`
   （不是 `cp`：运行中的库直接拷贝有损坏风险）
2. 备份失败 → **中止升级**，程序报错退出，数据库保持原样
3. 迁移全部成功后清理旧备份，只保留最近 5 份

也可以在「系统设置 → 维护」里随时手动生成一份备份。

### 2.3 密码迁移

v1 把密码明文存在 `app_settings.server_password`。v2 改用 bcrypt：

1. 第一次用旧密码登录成功时，自动把哈希写入 `server_password_hash`
2. **此时明文行会被保留**，目的是让升级当天仍然可以无损回滚到 v1
3. 用户下一次在「系统设置 → 账户」修改密码时，明文行会被清空

如果不打算回滚、希望立刻清掉明文，登录后修改一次密码即可。

> "记住登录"令牌的签名密钥从"用密码当密钥"改成了独立的随机
> `session_secret`。升级后第一次访问时旧的 `remember_token` 会失效，需要重新
> 登录一次，之后不再受影响。

### 2.4 环境变量

新旧变量都被接受，`DOMAINHUNTER_*` 优先：

| 新变量 | 旧变量（仍可用，已废弃） |
| --- | --- |
| `DOMAINHUNTER_WHOIS_FALLBACK_URL` | `PUFF_WHOIS_FALLBACK_URL` |
| `DOMAINHUNTER_WHOIS_FALLBACK_TLDS` | `PUFF_WHOIS_FALLBACK_TLDS` |
| `DOMAINHUNTER_WHOIS_FALLBACK_TIMEOUT` | `PUFF_WHOIS_FALLBACK_TIMEOUT` |
| `DOMAINHUNTER_WHOIS_LS_URL` | `PUFF_WHOIS_LS_URL` |
| `DOMAINHUNTER_WHOIS_LS_TLDS` | `PUFF_WHOIS_LS_TLDS` |
| `DOMAINHUNTER_WHOIS_LS_TIMEOUT` | `PUFF_WHOIS_LS_TIMEOUT` |
| `DOMAINHUNTER_RDAP_BOOTSTRAP_URL` | `PUFF_RDAP_BOOTSTRAP_URL` |
| `DOMAINHUNTER_COOKIE_SECURE` | `PUFF_COOKIE_SECURE` |
| `DOMAINHUNTER_CORS_ORIGINS` | `PUFF_CORS_ORIGINS` |
| `DOMAINHUNTER_CSRF_ENABLED` | `PUFF_CSRF_ENABLED` |

### 2.5 API 变化

旧接口全部保留，路径与响应结构不变：
`/api/domains`、`/api/domain/*`、`/api/stats`、`/api/settings/*`、
`/api/monitor/*`、`/health`。

唯一的行为调整（安全原因）：`GET /api/settings` 不再返回 SMTP 密码与
Telegram Bot Token 的明文，改为 `password_set` / `bot_token_set` 布尔值。
保存设置时把对应字段留空即表示"保持不变"。

新增能力放在 `/api/v2/*` 与 `/api/domains/{domain}/history`。

---

## 3. 升级步骤（Docker Compose）

```bash
# 1. 先备份（升级流程也会自动备份，但手工留一份更稳）
cp -a data/puff.db data/puff.db.pre-v2

# 2. 记录当前镜像，回滚时要用
docker inspect DomainHunter --format '{{.Image}}'

# 3. 构建新镜像（此时容器仍在跑，不停机）
docker compose build

# 4. 切换
docker compose up -d

# 5. 验证
curl -f http://127.0.0.1:8080/health
docker compose logs --tail 50
```

## 4. 回滚

### 4.1 只回滚程序（推荐，数据不动）

因为迁移只做加法，v1 可以直接读 v2 的库：

```bash
# 用记录下来的旧镜像 ID 覆盖 image 字段后重启，或者
docker tag <旧镜像ID> domainhunter-go:latest
docker compose up -d
```

登录用原来的密码即可（明文行仍在）。

### 4.2 连数据一起回滚

只有在数据出现问题时才需要：

```bash
docker compose stop domainhunter
cp -a data/backups/puff-YYYYMMDD-HHMMSS-pre-migration.db data/puff.db
rm -f data/puff.db-wal data/puff.db-shm
docker compose up -d
```

## 5. 验证清单

```
[ ] curl -f /health 返回 status=ok
[ ] 能登录，用户名密码不变
[ ] 域名列表数量与升级前一致
[ ] 随便点一个域名"立即检查"，能返回结果
[ ] 设置页能读到原来的 SMTP / Telegram 配置
[ ] docker logs 里没有 migration 相关报错
```
