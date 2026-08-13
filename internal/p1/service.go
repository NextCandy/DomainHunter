package p1

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/storage/sqlite"
)

// Service 是 P1 的聚合服务：筛选/视图、批量预览、AI 队列与自动化审计共用同一个 SQLite。
type Service struct {
	db *sqlite.DB
	ai *AIService

	automationMu      sync.Mutex
	automationCancel  context.CancelFunc
	automationWG      sync.WaitGroup
	automationStarted bool
}

func New(db *sqlite.DB) *Service {
	return &Service{db: db, ai: NewAIService(db)}
}

func (s *Service) Start(ctx context.Context) {
	s.ai.Start(ctx)

	s.automationMu.Lock()
	if s.automationStarted {
		s.automationMu.Unlock()
		return
	}
	eventCtx, cancel := context.WithCancel(ctx)
	s.automationCancel = cancel
	s.automationStarted = true
	s.automationWG.Add(1)
	s.automationMu.Unlock()

	if err := s.initializeAutomationCursors(eventCtx); err != nil {
		// P1 migration runs before Start. If a deployment has an unexpected
		// schema problem, keep AI available but do not run an unbounded replay.
		cancel()
		s.automationWG.Done()
		s.automationMu.Lock()
		s.automationCancel = nil
		s.automationStarted = false
		s.automationMu.Unlock()
		return
	}
	go s.automationLoop(eventCtx)
}

func (s *Service) Stop() {
	s.automationMu.Lock()
	if s.automationCancel != nil {
		s.automationCancel()
		s.automationCancel = nil
	}
	s.automationStarted = false
	s.automationMu.Unlock()
	s.automationWG.Wait()
	s.ai.Stop()
}

func (s *Service) AISettings(ctx context.Context) (AISettingsPublic, error) {
	return s.ai.PublicSettings(ctx)
}
func (s *Service) SaveAISettings(ctx context.Context, input AISettingsInput) (AISettingsPublic, error) {
	return s.ai.SaveSettings(ctx, input)
}
func (s *Service) AIProviderProfiles(ctx context.Context) ([]AIProviderProfilePublic, error) {
	return s.ai.ListProviderProfiles(ctx)
}
func (s *Service) CreateAIProviderProfile(ctx context.Context, input AISettingsInput) (AIProviderProfilePublic, error) {
	return s.ai.CreateProviderProfile(ctx, input)
}
func (s *Service) DeleteAIProviderProfile(ctx context.Context, id int64) error {
	return s.ai.DeleteProviderProfile(ctx, id)
}
func (s *Service) AIModels(ctx context.Context) ([]string, error) { return s.ai.ListModels(ctx) }
func (s *Service) AIUsage(ctx context.Context) (AIUsage, error)   { return s.ai.Usage(ctx) }
func (s *Service) AIJobs(ctx context.Context, limit int) ([]AIJob, error) {
	return s.ai.ListJobs(ctx, limit)
}
func (s *Service) EnqueueAI(ctx context.Context, names []string) (int, error) {
	return s.ai.Enqueue(ctx, names)
}
func (s *Service) CancelAIJob(ctx context.Context, id int64) error { return s.ai.CancelJob(ctx, id) }
func (s *Service) AIValuation(ctx context.Context, name string) (*Valuation, error) {
	return s.ai.GetValuation(ctx, name)
}

// AIQueryConfig exposes only the active profile material needed by the query
// fallback. The API key is consumed in memory by the caller and never returned
// by an HTTP handler or written to logs.
func (s *Service) AIQueryConfig(ctx context.Context) (baseURL, apiKey, model string, enabled bool, err error) {
	settings, encrypted, err := s.ai.storedSettings(ctx)
	if err != nil {
		return "", "", "", false, err
	}
	if err := validateAIBaseURL(settings.BaseURL); err != nil {
		return "", "", "", false, err
	}
	key, source := configuredAPIKey(encrypted)
	return settings.BaseURL, key, settings.Model, settings.Enabled && source != "encrypted_unavailable", nil
}

type automationObservationEvent struct {
	ID             int64
	Domain         string
	Status         string
	PreviousStatus string
	Changed        bool
}

func (s *Service) initializeAutomationCursors(ctx context.Context) error {
	var domainID, observationID int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM domains`).Scan(&domainID); err != nil {
		return err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM domain_observations`).Scan(&observationID); err != nil {
		return err
	}
	for key, value := range map[string]string{
		"domain_id":      strconv.FormatInt(domainID, 10),
		"observation_id": strconv.FormatInt(observationID, 10),
		"expiry_day":     "",
	} {
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO automation_cursors(key,value) VALUES(?,?)`, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) automationCursor(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM automation_cursors WHERE key=?`, key).Scan(&value)
	return value, err
}

func (s *Service) setAutomationCursor(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO automation_cursors(key,value,updated_at) VALUES(?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`, key, value)
	return err
}

func (s *Service) automationLoop(ctx context.Context) {
	defer s.automationWG.Done()
	_ = s.scanAutomationEvents(ctx)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.scanAutomationEvents(ctx)
		}
	}
}

func (s *Service) scanAutomationEvents(ctx context.Context) error {
	if err := s.scanNewDomainEvents(ctx); err != nil {
		return err
	}
	if err := s.scanObservationEvents(ctx); err != nil {
		return err
	}
	return s.scanExpiryEvents(ctx)
}

func (s *Service) scanNewDomainEvents(ctx context.Context) error {
	raw, err := s.automationCursor(ctx, "domain_id")
	if err != nil {
		return err
	}
	cursor, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("自动化域名游标无效: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,name FROM domains WHERE id>? ORDER BY id ASC LIMIT 100`, cursor)
	if err != nil {
		return err
	}
	type domainEvent struct {
		id   int64
		name string
	}
	var events []domainEvent
	for rows.Next() {
		var event domainEvent
		if err := rows.Scan(&event.id, &event.name); err != nil {
			rows.Close()
			return err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range events {
		if _, err := s.EvaluateAutomation(ctx, AutomationEvent{
			ID: fmt.Sprintf("domain:%d", item.id), Type: "domain_added", Domain: item.name,
		}, true); err != nil {
			return err
		}
		if err := s.setAutomationCursor(ctx, "domain_id", strconv.FormatInt(item.id, 10)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) scanObservationEvents(ctx context.Context) error {
	raw, err := s.automationCursor(ctx, "observation_id")
	if err != nil {
		return err
	}
	cursor, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("自动化观测游标无效: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT current.id,current.domain,current.status,COALESCE(current.changed,0),
		       COALESCE((SELECT previous.status
				FROM domain_observations AS previous
				WHERE lower(previous.domain)=lower(current.domain)
				  AND (previous.observed_at<current.observed_at OR
				       (previous.observed_at=current.observed_at AND previous.id<current.id))
				ORDER BY previous.observed_at DESC, previous.id DESC LIMIT 1),'')
		FROM domain_observations AS current
		WHERE current.id>? ORDER BY current.id ASC LIMIT 100`, cursor)
	if err != nil {
		return err
	}
	var events []automationObservationEvent
	for rows.Next() {
		var event automationObservationEvent
		var changed int
		if err := rows.Scan(&event.ID, &event.Domain, &event.Status, &changed, &event.PreviousStatus); err != nil {
			rows.Close()
			return err
		}
		event.Changed = changed == 1
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range events {
		pending := []AutomationEvent{{
			ID: fmt.Sprintf("observation:%d:completed", item.ID), Type: "observation_completed", Domain: item.Domain,
		}}
		if item.Changed {
			pending = append(pending, AutomationEvent{
				ID: fmt.Sprintf("observation:%d:changed", item.ID), Type: "status_changed", Domain: item.Domain,
			})
		}
		if strings.EqualFold(item.Status, "error") {
			pending = append(pending, AutomationEvent{
				ID: fmt.Sprintf("observation:%d:error", item.ID), Type: "error", Domain: item.Domain,
			})
		} else if item.Changed && strings.EqualFold(item.PreviousStatus, "error") {
			pending = append(pending, AutomationEvent{
				ID: fmt.Sprintf("observation:%d:recovery", item.ID), Type: "recovery", Domain: item.Domain,
			})
		}
		for _, event := range pending {
			if _, err := s.EvaluateAutomation(ctx, event, true); err != nil {
				return err
			}
		}
		if err := s.setAutomationCursor(ctx, "observation_id", strconv.FormatInt(item.ID, 10)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) scanExpiryEvents(ctx context.Context) error {
	today := time.Now().UTC().Format("2006-01-02")
	lastDay, err := s.automationCursor(ctx, "expiry_day")
	if err != nil {
		return err
	}
	if lastDay == today {
		return nil
	}
	now := time.Now().UTC()
	rows, err := s.db.QueryContext(ctx, `SELECT domain FROM domain_results
		WHERE expiry_at IS NOT NULL AND expiry_at>=? AND expiry_at<=?
		ORDER BY expiry_at ASC, domain ASC LIMIT 500`, now, now.Add(7*24*time.Hour))
	if err != nil {
		return err
	}
	var domains []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		domains = append(domains, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, name := range domains {
		if _, err := s.EvaluateAutomation(ctx, AutomationEvent{
			ID: fmt.Sprintf("expiry:%s:%s", today, strings.ToLower(name)), Type: "expiry_scan", Domain: name,
		}, true); err != nil {
			return err
		}
	}
	return s.setAutomationCursor(ctx, "expiry_day", today)
}

type richDomain struct {
	Info      domain.Info
	Enabled   bool
	Notify    bool
	FolderID  *int64
	CreatedAt time.Time
	AI        *Valuation
}

func (r richDomain) field(name string) (any, bool) {
	switch name {
	case "name":
		return r.Info.Name, true
	case "status":
		return string(r.Info.Status), true
	case "tld":
		return tldOf(r.Info.Name), true
	case "registrar":
		return r.Info.Registrar, true
	case "provider":
		return r.Info.QueryMethod, true
	case "tag":
		return r.Info.Tags, true
	case "folder_id":
		if r.FolderID == nil {
			return nil, true
		}
		return *r.FolderID, true
	case "favorite":
		return r.Info.Favorite, true
	case "enabled":
		return r.Enabled, true
	case "created_at":
		return &r.CreatedAt, true
	case "last_checked":
		if r.Info.LastChecked.IsZero() {
			return nil, true
		}
		return &r.Info.LastChecked, true
	case "expiry_at":
		return r.Info.ExpiryDate, true
	case "ai_quality":
		if r.AI == nil {
			return nil, true
		}
		return r.AI.QualityScore, true
	case "ai_liquidity":
		if r.AI == nil {
			return nil, true
		}
		return r.AI.LiquidityScore, true
	case "ai_risk":
		if r.AI == nil {
			return nil, true
		}
		return r.AI.RiskLevel, true
	case "ai_value_low":
		if r.AI == nil {
			return nil, true
		}
		return r.AI.ValueLow, true
	case "ai_value_high":
		if r.AI == nil {
			return nil, true
		}
		return r.AI.ValueHigh, true
	case "ai_confidence":
		if r.AI == nil {
			return nil, true
		}
		return r.AI.Confidence, true
	case "review_required":
		return r.Info.Review != nil && r.Info.Review.Required, true
	case "review_reason":
		if r.Info.Review == nil {
			return []string(nil), true
		}
		reasons := make([]string, 0, len(r.Info.Review.Reasons))
		for _, reason := range r.Info.Review.Reasons {
			reasons = append(reasons, string(reason))
		}
		return reasons, true
	default:
		return nil, false
	}
}

func (s *Service) loadDomains(ctx context.Context) ([]richDomain, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.name, d.enabled, d.notify, d.favorite, COALESCE(d.note,''),
		       COALESCE(d.tags,''), d.folder_id, d.created_at,
		       COALESCE(r.status,''), COALESCE(r.registrar,''), r.created_at, r.expiry_at,
		       r.updated_at, r.last_checked, COALESCE(r.query_method,''),
		       COALESCE(r.whois_raw,''), COALESCE(r.error_message,''),
		       COALESCE(r.name_servers,''), COALESCE(r.epp_statuses,''),
		       COALESCE(r.confidence,''), v.result_json
		FROM domains d
		LEFT JOIN domain_results r ON lower(r.domain) = lower(d.name)
		LEFT JOIN ai_domain_valuations v ON lower(v.domain) = lower(d.name)
		ORDER BY d.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("读取 P1 域名数据失败: %w", err)
	}
	defer rows.Close()
	var out []richDomain
	for rows.Next() {
		var (
			item                                                                                   richDomain
			name, note, tags, status, registrar, method, raw, errMessage, servers, epp, confidence string
			enabled, notify, favorite                                                              int
			folderID                                                                               sql.NullInt64
			createdAt, createdDate, expiryDate, updatedDate, lastChecked                           sql.NullTime
			resultJSON                                                                             sql.NullString
		)
		if err := rows.Scan(&name, &enabled, &notify, &favorite, &note, &tags, &folderID,
			&createdAt, &status, &registrar, &createdDate, &expiryDate, &updatedDate,
			&lastChecked, &method, &raw, &errMessage, &servers, &epp, &confidence, &resultJSON); err != nil {
			return nil, fmt.Errorf("解析 P1 域名数据失败: %w", err)
		}
		item.Info.Name = name
		item.Info.Status = domain.Status(status)
		if item.Info.Status == "" {
			item.Info.Status = domain.StatusUnknown
		}
		item.Info.Registrar, item.Info.QueryMethod = registrar, method
		item.Info.ErrorMessage, item.Info.WhoisRaw = errMessage, raw
		item.Info.Tags, item.Info.Note = splitCSV(tags), note
		item.Info.NameServers = splitCSV(servers)
		item.Info.EPPStatuses = splitCSV(epp)
		item.Info.Confidence = domain.Confidence(confidence)
		item.Enabled, item.Notify = enabled == 1, notify == 1
		item.Info.Favorite = favorite == 1
		item.Info.AddedAt = nullTimePtr(createdAt)
		item.CreatedAt = createdAt.Time
		if folderID.Valid {
			value := folderID.Int64
			item.FolderID = &value
			item.Info.FolderID = &value
		}
		item.Info.CreatedDate = nullTimePtr(createdDate)
		item.Info.ExpiryDate = nullTimePtr(expiryDate)
		item.Info.UpdatedDate = nullTimePtr(updatedDate)
		if lastChecked.Valid {
			item.Info.LastChecked = lastChecked.Time
		}
		item.Info.Review = domain.BuildReviewState(&item.Info, time.Now())
		if resultJSON.Valid {
			var valuation Valuation
			if json.Unmarshal([]byte(resultJSON.String), &valuation) == nil {
				item.AI = &valuation
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func tldOf(name string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(name)), ".")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func (s *Service) ListDomains(ctx context.Context, node FilterNode, page, limit int) (*DomainListResult, error) {
	items, err := s.loadDomains(ctx)
	if err != nil {
		return nil, err
	}
	filtered := items[:0]
	for _, item := range items {
		if filterMatches(node, item) {
			filtered = append(filtered, item)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Info.Name < filtered[j].Info.Name })
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 500 {
		limit = 20
	}
	total := len(filtered)
	start := (page - 1) * limit
	end := start + limit
	if start >= total {
		start, end = 0, 0
	} else if end > total {
		end = total
	}
	result := make([]*domain.Info, 0, end-start)
	for i := start; i < end; i++ {
		result = append(result, &filtered[i].Info)
	}
	out := &DomainListResult{
		Domains: result, Total: len(items), TotalFiltered: total, Page: page, Limit: limit,
		TotalPages: (total + limit - 1) / limit, HasNext: end < total, HasPrev: page > 1, DataStatus: "ok",
	}
	if len(items) == 0 {
		out.DataStatus = "empty"
	} else if len(result) == 0 {
		out.DataStatus = "no_results"
	}
	return out, nil
}

func (s *Service) matchingDomains(ctx context.Context, action BulkAction) ([]richDomain, error) {
	items, err := s.loadDomains(ctx)
	if err != nil {
		return nil, err
	}
	if len(action.Domains) > 0 {
		wanted := make(map[string]bool, len(action.Domains))
		for _, name := range action.Domains {
			wanted[strings.ToLower(strings.TrimSpace(name))] = true
		}
		filtered := items[:0]
		for _, item := range items {
			if wanted[strings.ToLower(item.Info.Name)] {
				filtered = append(filtered, item)
			}
		}
		return filtered, nil
	}
	filtered := items[:0]
	for _, item := range items {
		if filterMatches(action.Filter, item) {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (s *Service) PreviewBulk(ctx context.Context, action BulkAction) (*BulkPreview, error) {
	if err := validateBulkAction(action); err != nil {
		return nil, err
	}
	items, err := s.matchingDomains(ctx, action)
	if err != nil {
		return nil, err
	}
	preview := &BulkPreview{ActionType: action.Type, Matched: len(items), Samples: make([]string, 0, 10), TaskCount: len(items)}
	for i, item := range items {
		if i < 10 {
			preview.Samples = append(preview.Samples, item.Info.Name)
		}
	}
	if action.Type == "ai_valuation" {
		preview.CacheHits = 0
		for _, item := range items {
			if item.AI != nil && item.AI.ExpiresAt.After(time.Now()) {
				preview.CacheHits++
			}
		}
		preview.TaskCount = len(items) - preview.CacheHits
	}
	return preview, nil
}

func validateBulkAction(action BulkAction) error {
	if len(action.Domains) > 500 {
		return fmt.Errorf("批量操作一次最多500个域名")
	}
	switch action.Type {
	case "tag":
		if strings.TrimSpace(action.Tag) == "" || len(action.Tag) > 64 {
			return fmt.Errorf("标签不能为空且不能超过64个字符")
		}
	case "priority":
		if action.Priority == nil || *action.Priority < 0 || *action.Priority > 1000 {
			return fmt.Errorf("优先级必须在0到1000之间")
		}
	case "folder":
	case "notification":
		if action.Notify == nil {
			return fmt.Errorf("缺少通知开关")
		}
	case "monitor":
		if action.Enabled == nil {
			return fmt.Errorf("缺少监控开关")
		}
	case "ai_valuation":
	default:
		return fmt.Errorf("不支持的批量动作: %s", action.Type)
	}
	return nil
}

type contextExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertBulkAudit(ctx context.Context, execer contextExecer, action BulkAction, matched, taskCount int, result map[string]any) error {
	input, err := json.Marshal(action)
	if err != nil {
		return fmt.Errorf("序列化批量审计输入失败: %w", err)
	}
	output, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("序列化批量审计结果失败: %w", err)
	}
	_, err = execer.ExecContext(ctx, `INSERT INTO bulk_action_audits(action_type,input_json,matched,task_count,result_json) VALUES(?,?,?,?,?)`,
		action.Type, string(input), matched, taskCount, string(output))
	return err
}

func (s *Service) recordBulkAudit(ctx context.Context, action BulkAction, matched, taskCount int, result map[string]any) error {
	return insertBulkAudit(ctx, s.db, action, matched, taskCount, result)
}

func (s *Service) ExecuteBulk(ctx context.Context, action BulkAction) (map[string]any, error) {
	if err := validateBulkAction(action); err != nil {
		return nil, err
	}
	items, err := s.matchingDomains(ctx, action)
	if err != nil {
		return nil, err
	}
	if action.Type == "ai_valuation" {
		names := make([]string, 0, len(items))
		for _, item := range items {
			names = append(names, item.Info.Name)
		}
		queued := 0
		for start := 0; start < len(names); start += 100 {
			end := start + 100
			if end > len(names) {
				end = len(names)
			}
			batchQueued, err := s.ai.Enqueue(ctx, names[start:end])
			if err != nil {
				return nil, err
			}
			queued += batchQueued
		}
		result := map[string]any{"status": "accepted", "updated": 0, "queued": queued}
		if err := s.recordBulkAudit(ctx, action, len(items), queued, result); err != nil {
			return nil, err
		}
		return result, nil
	}
	if len(items) == 0 {
		result := map[string]any{"status": "success", "updated": 0}
		if err := s.recordBulkAudit(ctx, action, 0, 0, result); err != nil {
			return nil, err
		}
		return result, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, item := range items {
		var stmt string
		var args []any
		switch action.Type {
		case "tag":
			stmt, args = `UPDATE domains SET tags = CASE WHEN instr(',' || lower(COALESCE(tags,'')) || ',', ',' || lower(?) || ',') > 0 THEN tags WHEN COALESCE(tags,'') = '' THEN ? ELSE tags || ',' || ? END WHERE lower(name)=lower(?)`, []any{action.Tag, action.Tag, action.Tag, item.Info.Name}
		case "priority":
			stmt, args = `UPDATE domains SET priority=? WHERE lower(name)=lower(?)`, []any{*action.Priority, item.Info.Name}
		case "folder":
			if action.FolderID == nil {
				stmt, args = `UPDATE domains SET folder_id=NULL WHERE lower(name)=lower(?)`, []any{item.Info.Name}
			} else {
				stmt, args = `UPDATE domains SET folder_id=? WHERE lower(name)=lower(?)`, []any{*action.FolderID, item.Info.Name}
			}
		case "notification":
			stmt, args = `UPDATE domains SET notify=? WHERE lower(name)=lower(?)`, []any{boolInt(*action.Notify), item.Info.Name}
		case "monitor":
			stmt, args = `UPDATE domains SET enabled=? WHERE lower(name)=lower(?)`, []any{boolInt(*action.Enabled), item.Info.Name}
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return nil, err
		}
	}
	result := map[string]any{"status": "success", "updated": len(items)}
	// Keep ordinary domain mutations and their audit record in one transaction.
	// The audit is inserted before commit so a failed audit cannot leave an
	// apparently successful, unaudited bulk mutation behind.
	if err := insertBulkAudit(ctx, tx, action, len(items), len(items), result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ListBulkAudits(ctx context.Context, limit int) ([]BulkAudit, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,action_type,matched,task_count,result_json,created_at
		FROM bulk_action_audits ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BulkAudit
	for rows.Next() {
		var item BulkAudit
		var raw string
		if err := rows.Scan(&item.ID, &item.ActionType, &item.Matched, &item.TaskCount, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &item.Result); err != nil {
			return nil, fmt.Errorf("解析批量审计结果失败: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func marshalFilter(node FilterNode) (string, error) {
	if node.Version == 0 {
		node.Version = filterVersion
	}
	if err := validateFilter(node, 0); err != nil {
		return "", err
	}
	data, err := json.Marshal(node)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Service) ListSavedViews(ctx context.Context) ([]SavedView, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,filter_json,shared,created_by,created_at,updated_at FROM saved_views ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedView
	for rows.Next() {
		var item SavedView
		var raw string
		var shared int
		if err := rows.Scan(&item.ID, &item.Name, &raw, &shared, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Shared = shared == 1
		item.Filter, err = ParseFilter(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) CreateSavedView(ctx context.Context, name string, node FilterNode, shared bool, owner string) (*SavedView, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return nil, fmt.Errorf("视图名称不能为空且不能超过120个字符")
	}
	raw, err := marshalFilter(node)
	if err != nil {
		return nil, err
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO saved_views(name,filter_json,shared,created_by,updated_at) VALUES(?,?,?,?,CURRENT_TIMESTAMP)`, name, raw, boolInt(shared), owner)
	if err != nil {
		return nil, fmt.Errorf("保存智能视图失败: %w", err)
	}
	id, _ := result.LastInsertId()
	return s.GetSavedView(ctx, id)
}

func (s *Service) GetSavedView(ctx context.Context, id int64) (*SavedView, error) {
	var item SavedView
	var raw string
	var shared int
	err := s.db.QueryRowContext(ctx, `SELECT id,name,filter_json,shared,created_by,created_at,updated_at FROM saved_views WHERE id=?`, id).Scan(&item.ID, &item.Name, &raw, &shared, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	item.Shared = shared == 1
	item.Filter, err = ParseFilter(raw)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) UpdateSavedView(ctx context.Context, id int64, name string, node FilterNode, shared bool) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return fmt.Errorf("视图名称不能为空且不能超过120个字符")
	}
	raw, err := marshalFilter(node)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE saved_views SET name=?,filter_json=?,shared=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, name, raw, boolInt(shared), id)
	return err
}
func (s *Service) DeleteSavedView(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM saved_views WHERE id=?`, id)
	return err
}
