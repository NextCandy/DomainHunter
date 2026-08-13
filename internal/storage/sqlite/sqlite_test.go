package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/repository"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return db
}

func TestMigrateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("首次迁移失败: %v", err)
	}
	applied, err := db.AppliedMigrations()
	if err != nil {
		t.Fatalf("读取迁移记录失败: %v", err)
	}
	if len(applied) != len(migrations) {
		t.Fatalf("应应用 %d 条迁移，实际 %d", len(migrations), len(applied))
	}
	db.Close()

	// 重新打开并再次迁移，不应重复执行也不应报错
	db2, err := Open(dir)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	defer db2.Close()
	if err := db2.Migrate(); err != nil {
		t.Fatalf("重复迁移失败: %v", err)
	}
	applied2, _ := db2.AppliedMigrations()
	if len(applied2) != len(applied) {
		t.Fatalf("重复迁移产生了额外记录: %d -> %d", len(applied), len(applied2))
	}
}

func TestUnlimitedAIMigrationClearsLegacyDailyLimits(t *testing.T) {
	db := newTestDB(t)
	var strictLimit, providerLimit, settingsLimit int
	if err := db.QueryRow(`SELECT COALESCE(MAX(daily_limit),0) FROM ai_profiles WHERE is_default=1`).Scan(&strictLimit); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT daily_limit FROM ai_provider_profiles WHERE is_default=1`).Scan(&providerLimit); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT daily_limit FROM ai_provider_settings WHERE id=1`).Scan(&settingsLimit); err != nil {
		t.Fatal(err)
	}
	if strictLimit != 0 || providerLimit != 0 || settingsLimit != 0 {
		t.Fatalf("legacy AI daily limits were not cleared: strict=%d provider=%d settings=%d", strictLimit, providerLimit, settingsLimit)
	}
}

func TestResolveFileDefaultsToDomainHunterDatabase(t *testing.T) {
	dir := t.TempDir()
	if got := ResolveFile(dir); got != DefaultFile {
		t.Fatalf("全新安装应创建 %s，实际 %s", DefaultFile, got)
	}

	t.Setenv("DOMAINHUNTER_DB_FILE", "custom.db")
	if got := ResolveFile(dir); got != "custom.db" {
		t.Fatalf("环境变量应优先，实际 %s", got)
	}
}

func TestMigrateMarksExistingBaseline(t *testing.T) {
	dir := t.TempDir()
	raw, err := sql.Open("sqlite", filepath.Join(dir, DefaultFile))
	if err != nil {
		t.Fatalf("创建旧库失败: %v", err)
	}
	for _, stmt := range migrations[0].Stmts {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("建表失败: %v", err)
		}
	}
	if _, err := raw.Exec(`INSERT INTO domains(name, enabled, notify) VALUES('legacy.com', 1, 1)`); err != nil {
		t.Fatalf("写入旧数据失败: %v", err)
	}
	raw.Close()

	db, err := Open(dir)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("迁移旧库失败: %v", err)
	}

	repo := NewDomainRepo(db)
	entry, err := repo.Get(context.Background(), "legacy.com")
	if err != nil || entry == nil {
		t.Fatalf("旧数据在迁移后丢失: %v %v", entry, err)
	}
	if !entry.Enabled {
		t.Fatal("旧域名的启用状态被破坏")
	}
}

func TestMigrateCreatesBackup(t *testing.T) {
	db := newTestDB(t)
	backups, err := db.ListBackups()
	if err != nil {
		t.Fatalf("列出备份失败: %v", err)
	}
	if len(backups) == 0 {
		t.Fatal("迁移前应当生成数据库备份")
	}
}

func TestBackupAndPrune(t *testing.T) {
	db := newTestDB(t)
	for i := 0; i < 7; i++ {
		if _, err := db.Backup("test"); err != nil {
			t.Fatalf("备份失败: %v", err)
		}
		time.Sleep(1100 * time.Millisecond)
	}
	if err := db.PruneBackups(5); err != nil {
		t.Fatalf("清理备份失败: %v", err)
	}
	backups, _ := db.ListBackups()
	if len(backups) > 5 {
		t.Fatalf("清理后应最多保留 5 份，实际 %d", len(backups))
	}
}

func TestDomainCreateGetDelete(t *testing.T) {
	db := newTestDB(t)
	repo := NewDomainRepo(db)
	results := NewResultRepo(db)
	ctx := context.Background()

	if err := repo.Create(ctx, "Example.COM", true, true); err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	entry, err := repo.Get(ctx, "example.com")
	if err != nil || entry == nil {
		t.Fatalf("读取域名失败: %v %v", entry, err)
	}
	if entry.Name != "example.com" {
		t.Fatalf("域名应被归一化为小写，实际 %q", entry.Name)
	}

	enabled, err := repo.IsEnabled(ctx, "example.com")
	if err != nil || !enabled {
		t.Fatalf("域名应处于启用状态: %v %v", enabled, err)
	}

	if err := results.Save(ctx, domain.Info{
		Name: "example.com", Status: domain.StatusRegistered,
		Registrar: "Example", LastChecked: time.Now(), QueryMethod: "rdap",
	}); err != nil {
		t.Fatalf("保存结果失败: %v", err)
	}
	info, err := results.Get(ctx, "example.com")
	if err != nil || info == nil || info.Status != domain.StatusRegistered {
		t.Fatalf("结果读取错误: %+v %v", info, err)
	}

	if err := repo.Delete(ctx, "example.com"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if entry, _ := repo.Get(ctx, "example.com"); entry != nil {
		t.Fatal("删除后仍能读到域名")
	}
	if info, _ := results.Get(ctx, "example.com"); info != nil {
		t.Fatal("删除域名后查询结果应一并删除")
	}
}

func TestObservationHistoryAndPrune(t *testing.T) {
	db := newTestDB(t)
	domains := NewDomainRepo(db)
	observations := NewObservationRepo(db)
	ctx := context.Background()

	if err := domains.Create(ctx, "history.com", true, true); err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	entry, _ := domains.Get(ctx, "history.com")

	for i := 0; i < 5; i++ {
		obs := domain.Observation{
			DomainID: entry.ID, Domain: entry.Name, Status: domain.StatusRegistered,
			Provider: "rdap", Confidence: domain.ConfidenceHigh,
			ObservedAt: time.Now().Add(time.Duration(-i) * time.Hour), Changed: i == 0,
		}
		attempts := []domain.Attempt{{
			DomainID: entry.ID, Domain: entry.Name, Provider: "rdap",
			Status: domain.StatusRegistered, Success: true, LatencyMS: 120,
			QueriedAt: obs.ObservedAt,
		}}
		if _, err := observations.Save(ctx, obs, attempts); err != nil {
			t.Fatalf("保存观测失败: %v", err)
		}
	}

	history, err := observations.ListByDomain(ctx, "history.com", 10)
	if err != nil || len(history) != 5 {
		t.Fatalf("观测历史数量不对: %d %v", len(history), err)
	}
	attempts, err := observations.ListAttempts(ctx, "history.com", 10)
	if err != nil || len(attempts) != 5 {
		t.Fatalf("查询尝试数量不对: %d %v", len(attempts), err)
	}
	changes, err := observations.ListRecentChanges(ctx, 10)
	if err != nil || len(changes) != 1 {
		t.Fatalf("状态变化记录数量不对: %d %v", len(changes), err)
	}

	removedObs, removedAttempts, err := observations.Prune(ctx, repository.Retention{MaxPerDomain: 2})
	if err != nil {
		t.Fatalf("清理历史失败: %v", err)
	}
	if removedObs != 3 {
		t.Fatalf("应清理 3 条观测，实际 %d", removedObs)
	}
	if removedAttempts != 3 {
		t.Fatalf("应级联清理 3 条查询尝试，实际 %d", removedAttempts)
	}

	history, _ = observations.ListByDomain(ctx, "history.com", 10)
	if len(history) != 2 {
		t.Fatalf("清理后应剩 2 条，实际 %d", len(history))
	}
}

func TestObservationAnalyticsAggregatesDailyStatusCountsAndChanges(t *testing.T) {
	db := newTestDB(t)
	domains := NewDomainRepo(db)
	observations := NewObservationRepo(db)
	ctx := context.Background()
	if err := domains.Create(ctx, "trend.com", true, true); err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	entry, err := domains.Get(ctx, "trend.com")
	if err != nil || entry == nil {
		t.Fatalf("读取域名失败: %v %v", entry, err)
	}

	zone := time.FixedZone("CST", 8*60*60)
	observationsToSave := []domain.Observation{
		{DomainID: entry.ID, Domain: entry.Name, Status: domain.StatusRegistered,
			Provider: "rdap", Confidence: domain.ConfidenceHigh,
			ObservedAt: time.Date(2026, 8, 9, 23, 0, 0, 0, zone)},
		{DomainID: entry.ID, Domain: entry.Name, Status: domain.StatusAvailable,
			Provider: "rdap", Confidence: domain.ConfidenceHigh, Changed: true,
			ObservedAt: time.Date(2026, 8, 10, 8, 0, 0, 0, zone)},
		{DomainID: entry.ID, Domain: entry.Name, Status: domain.StatusError,
			Provider: "rdap", Confidence: domain.ConfidenceLow, Changed: true,
			ObservedAt: time.Date(2026, 8, 10, 9, 0, 0, 0, zone)},
		{DomainID: entry.ID, Domain: entry.Name, Status: domain.StatusRegistered,
			Provider: "rdap", Confidence: domain.ConfidenceHigh, Changed: true,
			ObservedAt: time.Date(2026, 8, 11, 8, 0, 0, 0, zone)},
	}
	for _, obs := range observationsToSave {
		if _, err := observations.Save(ctx, obs, nil); err != nil {
			t.Fatalf("保存观测失败: %v", err)
		}
	}

	counts, err := observations.DailyStatusCounts(ctx, "2026-08-10", "2026-08-11")
	if err != nil {
		t.Fatalf("读取每日状态计数失败: %v", err)
	}
	if len(counts) != 3 {
		t.Fatalf("应返回 3 个日状态分组，实际 %d: %+v", len(counts), counts)
	}
	if counts[0].Day != "2026-08-10" || counts[0].Status != domain.StatusAvailable || counts[0].Count != 1 || counts[0].Changed != 1 {
		t.Fatalf("8 月 10 日可注册分组错误: %+v", counts[0])
	}

	changes, err := observations.ChangesBetween(ctx, "2026-08-10", "2026-08-10")
	if err != nil {
		t.Fatalf("读取状态变化失败: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("8 月 10 日应有 2 条状态变化，实际 %d", len(changes))
	}
	if changes[0].Observation.Status != domain.StatusAvailable || changes[0].OldStatus != domain.StatusRegistered {
		t.Fatalf("第一条变化的前后状态错误: %+v", changes[0])
	}
}

func TestScheduleAndBackfill(t *testing.T) {
	db := newTestDB(t)
	repo := NewDomainRepo(db)
	ctx := context.Background()

	if err := repo.Create(ctx, "due.com", true, true); err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	if err := repo.ScheduleNext(ctx, "due.com", time.Now().Add(-time.Minute), 0); err != nil {
		t.Fatalf("写入下次检查时间失败: %v", err)
	}
	if err := repo.Create(ctx, "future.com", true, true); err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	if err := repo.ScheduleNext(ctx, "future.com", time.Now().Add(time.Hour), 0); err != nil {
		t.Fatalf("写入下次检查时间失败: %v", err)
	}

	due, err := repo.DueForCheck(ctx, time.Now(), 10)
	if err != nil {
		t.Fatalf("查询到期域名失败: %v", err)
	}
	if len(due) != 1 || due[0].Name != "due.com" {
		t.Fatalf("到期域名筛选错误: %+v", due)
	}

	// 禁用的域名不应被调度
	disabledFlag := false
	if err := repo.Update(ctx, "due.com", repository.DomainPatch{Enabled: &disabledFlag}); err != nil {
		t.Fatalf("禁用域名失败: %v", err)
	}
	due, _ = repo.DueForCheck(ctx, time.Now(), 10)
	if len(due) != 0 {
		t.Fatalf("禁用的域名不应进入调度: %+v", due)
	}
}

func TestBackfillScheduleFillsMissing(t *testing.T) {
	db := newTestDB(t)
	repo := NewDomainRepo(db)
	results := NewResultRepo(db)
	ctx := context.Background()

	if _, err := db.Exec(`INSERT INTO domains(name, enabled, notify) VALUES('nosched.com', 1, 1)`); err != nil {
		t.Fatalf("插入无调度时间的域名失败: %v", err)
	}
	// 最近才查过的域名应沿用 last_checked + 间隔，而不是被立刻重查
	if _, err := db.Exec(`INSERT INTO domains(name, enabled, notify) VALUES('recent.com', 1, 1)`); err != nil {
		t.Fatalf("插入域名失败: %v", err)
	}
	if err := results.Save(ctx, domain.Info{
		Name: "recent.com", Status: domain.StatusRegistered, LastChecked: time.Now(),
	}); err != nil {
		t.Fatalf("写入结果失败: %v", err)
	}

	n, err := repo.BackfillSchedule(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("补齐调度时间失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("应补齐 2 条，实际 %d", n)
	}

	entry, _ := repo.Get(ctx, "nosched.com")
	if entry.NextCheckAt == nil {
		t.Fatal("补齐后 next_check_at 仍为空")
	}
	recent, _ := repo.Get(ctx, "recent.com")
	if recent.NextCheckAt == nil || !recent.NextCheckAt.After(time.Now().Add(4*time.Minute)) {
		t.Fatalf("刚查过的域名不应立刻重查: %v", recent.NextCheckAt)
	}
}

// TestBackfillScheduleSpreadsOverdueDomains 确认升级后不会出现"几百个域名
// 同一秒全部到期"的查询风暴
func TestBackfillScheduleSpreadsOverdueDomains(t *testing.T) {
	db := newTestDB(t)
	repo := NewDomainRepo(db)
	ctx := context.Background()

	const total = 100
	for i := 0; i < total; i++ {
		if _, err := db.Exec(`INSERT INTO domains(name, enabled, notify) VALUES(?, 1, 1)`,
			fmt.Sprintf("bulk%03d.com", i)); err != nil {
			t.Fatalf("插入域名失败: %v", err)
		}
	}

	interval := 10 * time.Minute
	if _, err := repo.BackfillSchedule(ctx, interval); err != nil {
		t.Fatalf("补齐调度时间失败: %v", err)
	}

	due, err := repo.DueForCheck(ctx, time.Now(), 1000)
	if err != nil {
		t.Fatalf("查询到期域名失败: %v", err)
	}
	if len(due) > 10 {
		t.Fatalf("到期域名应被打散，当前一次性到期 %d 个", len(due))
	}

	all, err := repo.List(ctx, true)
	if err != nil {
		t.Fatalf("读取域名失败: %v", err)
	}
	latest := time.Now()
	for _, entry := range all {
		if entry.NextCheckAt != nil && entry.NextCheckAt.After(latest) {
			latest = *entry.NextCheckAt
		}
	}
	if latest.After(time.Now().Add(interval)) {
		t.Fatalf("打散后最晚的检查时间不应超出一个间隔窗口: %v", latest)
	}
}

func TestCleanOrphaned(t *testing.T) {
	db := newTestDB(t)
	domains := NewDomainRepo(db)
	results := NewResultRepo(db)
	ctx := context.Background()

	if err := results.Save(ctx, domain.Info{
		Name: "orphan.com", Status: domain.StatusRegistered, LastChecked: time.Now(),
	}); err != nil {
		t.Fatalf("写入孤立结果失败: %v", err)
	}
	deleted, _, err := domains.CleanOrphaned(ctx)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("应清理 1 条孤立结果，实际 %d", deleted)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	db := newTestDB(t)
	repo := NewSettingsRepo(db)
	ctx := context.Background()

	if err := repo.Upsert(ctx, map[string]string{"a": "1", "b": "2"}); err != nil {
		t.Fatalf("写入设置失败: %v", err)
	}
	if err := repo.Upsert(ctx, map[string]string{"a": "3"}); err != nil {
		t.Fatalf("更新设置失败: %v", err)
	}
	value, ok, err := repo.Get(ctx, "a")
	if err != nil || !ok || value != "3" {
		t.Fatalf("设置读取错误: %q %v %v", value, ok, err)
	}
	all, _ := repo.All(ctx)
	if all["b"] != "2" {
		t.Fatalf("设置丢失: %+v", all)
	}
}

func TestNotificationHistory(t *testing.T) {
	db := newTestDB(t)
	repo := NewNotificationRepo(db)
	ctx := context.Background()

	if err := repo.Save(ctx, "notify.com", "available", "registered"); err != nil {
		t.Fatalf("保存通知失败: %v", err)
	}
	last, err := repo.Last(ctx, "notify.com")
	if err != nil || last == nil || last.Status != "available" {
		t.Fatalf("通知记录读取错误: %+v %v", last, err)
	}
	records, err := repo.ListRecent(ctx, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("通知历史数量不对: %d %v", len(records), err)
	}
}

func TestNotificationDigestConfigDefaultsAndPersistsLastSentAt(t *testing.T) {
	db := newTestDB(t)
	repo := NewNotificationConfigRepo(db)
	ctx := context.Background()

	digest, err := repo.GetDigest(ctx)
	if err != nil {
		t.Fatalf("读取默认摘要配置失败: %v", err)
	}
	if digest.Hour != 8 || digest.Minute != 0 || digest.Enabled {
		t.Fatalf("摘要默认配置错误: %+v", digest)
	}

	when := time.Date(2026, 8, 11, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	if err := repo.MarkDigestSent(ctx, when); err != nil {
		t.Fatalf("保存摘要发送时间失败: %v", err)
	}
	digest, err = repo.GetDigest(ctx)
	if err != nil || digest.LastSentAt.IsZero() || !digest.LastSentAt.Equal(when) {
		t.Fatalf("摘要发送时间持久化错误: %+v %v", digest, err)
	}
}
