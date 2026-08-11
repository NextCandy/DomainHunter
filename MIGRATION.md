# 升级与回滚指南

本文覆盖三个阶段：

```
旧版 Puff  →  当前 DomainHunter(v1)  →  重构版 DomainHunter(v2)
```

## 结论先说

- **既有的 `data/puff.db` 会被自动识别并继续使用，不会被改名**。全新安装才创建
  `data/domainhunter.db`。想手工改名见 §2.6。
- **不需要重建域名列表、账号或通知设置**，全部沿用数据库里的值。
- **旧的 `PUFF_*` 环境变量继续有效**，可以先升级程序、再逐步改环境变量。
- 升级时会**自动生成一份数据库备份**再执行迁移；备份失败则中止升级。
- 仓库与镜像已从 `DomainHunter-go` 改名为 `DomainHunter`，见 §2.7。

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
| 006 | epp_statuses | `domain_results.epp_statuses` |
| 007 | folders | `folders` 表与 `domains.folder_id` |
| 008 | api_tokens | Bearer token 哈希存储 |
| 009 | notification_rules_templates_digest | 通知规则、模板与摘要配置 |
| 010 | p1_saved_views_ai_automation | 智能视图、AI Provider/Job/估价、自动化规则/运行审计 |

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

P1 AI 变量没有旧版别名：

| 变量 | 说明 |
| --- | --- |
| `DOMAINHUNTER_AI_API_KEY` | 优先级最高的环境 Key，不写入 SQLite |
| `DOMAINHUNTER_SECRET_KEY` | UI 保存 Key 时用于 AES-GCM 加密的主密钥；不要提交到仓库 |
| `DOMAINHUNTER_AI_ALLOWED_HOSTS` | 手动 Base URL 的主机 allowlist；默认允许 `api.deepseek.com` |
| `DOMAINHUNTER_AI_ALLOW_INSECURE_LOCAL` | 仅设为 `true` 才允许受控本机 HTTP 开发端点 |

### 2.5 API 变化

旧接口全部保留，路径与响应结构不变：
`/api/domains`、`/api/domain/*`、`/api/stats`、`/api/settings/*`、
`/api/monitor/*`、`/health`。

唯一的行为调整（安全原因）：`GET /api/settings` 不再返回 SMTP 密码与
Telegram Bot Token 的明文，改为 `password_set` / `bot_token_set` 布尔值。
保存设置时把对应字段留空即表示"保持不变"。

新增能力放在 `/api/v2/*` 与 `/api/domains/{domain}/history`。
P1 新增 `saved-views`、`bulk-actions`、`ai/*` 与 `automation/*`，均需登录和 CSRF；
AI Key 读取只返回 `api_key_set` / `key_source`。

### 2.6 数据库文件名

选择顺序：

1. `DOMAINHUNTER_DB_FILE` 环境变量
2. 已存在的 `puff.db`（旧版遗留，**程序绝不自动改名**）
3. 已存在的 `domainhunter.db`
4. 都不存在（全新安装）→ 创建 `domainhunter.db`

改名是可选的手动操作，程序两种名字都支持。要改就必须停机做，
运行中改名会让已打开的文件句柄与新文件脱节：

```bash
docker compose stop domainhunter
cd data
cp -a puff.db backups/puff-$(date +%Y%m%d-%H%M%S)-pre-db-rename.db   # 回滚点
mv puff.db domainhunter.db
rm -f puff.db-wal puff.db-shm
sqlite3 domainhunter.db "PRAGMA integrity_check; SELECT COUNT(*) FROM domains;"
docker compose up -d
```

回滚就是把 `domainhunter.db` 改回 `puff.db`（或直接用上面那份回滚点覆盖）。
树莓派部署已在 2026-08-11 执行完这步，现在用的是 `domainhunter.db`。

### 2.7 仓库与镜像改名

| 项目 | 原 | 现 |
| --- | --- | --- |
| GitHub 仓库 | `NextCandy/DomainHunter-go` | `NextCandy/DomainHunter` |
| 本地镜像 | `domainhunter-go` | `domainhunter` |
| GHCR 镜像 | `ghcr.io/nextcandy/domainhunter` | 不变 |

GitHub 会自动把旧仓库地址重定向到新地址，但建议更新本地 remote：

```bash
git remote set-url origin https://github.com/NextCandy/DomainHunter.git
```

Compose 里如果写的是 `image: domainhunter-go:latest`，改成 `domainhunter:latest`
并重新 build 即可；旧镜像仍在本地，可作为回滚点。

### 2.8 通知渠道

除邮件与 Telegram 外新增 Bark、飞书机器人、自定义 Webhook。

**原 Puff 的 `bark_url` / `bark_enabled` 设置会直接生效**，格式完全一致
（完整推送地址，含设备 key，自建服务器同样适用），无需重新配置。

---

## 3. 升级步骤（Docker Compose）

```bash
# 1. 先备份（升级流程也会自动备份，但手工留一份更稳）
#    <库名> = domainhunter.db，旧部署未改名时是 puff.db
cp -a data/<库名> data/<库名>.pre-v2

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
docker tag <旧镜像ID> domainhunter:latest
docker compose up -d
```

登录用原来的密码即可（明文行仍在）。

### 4.2 连数据一起回滚

只有在数据出现问题时才需要：

```bash
docker compose stop domainhunter
# 用 data/backups/ 里迁移前那份覆盖回去，文件名跟当前实际使用的库保持一致
cp -a data/backups/<库名>-YYYYMMDD-HHMMSS-pre-migration.db data/<库名>
rm -f data/<库名>-wal data/<库名>-shm
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
[ ] P1 迁移 010 已应用，`PRAGMA integrity_check` 返回 `ok`
[ ] 未登录访问 P1 受保护接口返回 401
```
