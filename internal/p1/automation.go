package p1

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type AutomationEvent struct {
	ID     string `json:"event_id"`
	Type   string `json:"type"`
	Domain string `json:"domain,omitempty"`
}

func (s *Service) ListAutomationRules(ctx context.Context) ([]AutomationRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,enabled,dry_run,trigger_json,conditions_json,actions_json,cooldown_seconds,daily_run_cap,created_at,updated_at FROM automation_rules ORDER BY updated_at DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AutomationRule
	for rows.Next() {
		var rule AutomationRule
		var enabled, dry int
		var triggerRaw, conditionsRaw, actionsRaw string
		if err := rows.Scan(&rule.ID, &rule.Name, &enabled, &dry, &triggerRaw, &conditionsRaw, &actionsRaw, &rule.CooldownSeconds, &rule.DailyRunCap, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		rule.Enabled, rule.DryRun = enabled == 1, dry == 1
		if err := json.Unmarshal([]byte(triggerRaw), &rule.Trigger); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(conditionsRaw), &rule.Conditions); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(actionsRaw), &rule.Actions); err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}

func validateAutomationRule(rule AutomationRule) error {
	rule.Name = strings.TrimSpace(rule.Name)
	if rule.Name == "" || len(rule.Name) > 120 {
		return fmt.Errorf("规则名称不能为空且不能超过120个字符")
	}
	if _, err := marshalFilter(rule.Trigger); err != nil {
		return fmt.Errorf("触发器无效: %w", err)
	}
	if _, err := marshalFilter(rule.Conditions); err != nil {
		return fmt.Errorf("条件无效: %w", err)
	}
	if len(rule.Actions) == 0 || len(rule.Actions) > 10 {
		return fmt.Errorf("规则必须包含1到10个安全动作")
	}
	if rule.CooldownSeconds < 0 || rule.CooldownSeconds > 30*24*3600 {
		return fmt.Errorf("冷却时间超出范围")
	}
	if rule.DailyRunCap < 1 || rule.DailyRunCap > 10000 {
		return fmt.Errorf("每日运行上限必须在1到10000之间")
	}
	for _, action := range rule.Actions {
		typeName, _ := action["type"].(string)
		switch typeName {
		case "tag", "priority", "folder", "notification", "monitor", "ai_valuation", "check", "send_notification":
		default:
			return fmt.Errorf("自动化动作不在安全白名单: %s", typeName)
		}
		if typeName == "tag" {
			tag, _ := action["tag"].(string)
			if strings.TrimSpace(tag) == "" || len(tag) > 64 {
				return fmt.Errorf("自动化标签无效")
			}
		}
		if typeName == "priority" {
			value, ok := numberValue(action["priority"])
			if !ok || value < 0 || value > 1000 {
				return fmt.Errorf("自动化优先级无效")
			}
		}
	}
	return nil
}

func (s *Service) CreateAutomationRule(ctx context.Context, rule AutomationRule) (*AutomationRule, error) {
	if err := validateAutomationRule(rule); err != nil {
		return nil, err
	}
	trigger, _ := json.Marshal(rule.Trigger)
	conditions, _ := json.Marshal(rule.Conditions)
	actions, _ := json.Marshal(rule.Actions)
	result, err := s.db.ExecContext(ctx, `INSERT INTO automation_rules(name,enabled,dry_run,trigger_json,conditions_json,actions_json,cooldown_seconds,daily_run_cap,updated_at) VALUES(?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`, rule.Name, boolInt(rule.Enabled), boolInt(rule.DryRun), string(trigger), string(conditions), string(actions), rule.CooldownSeconds, rule.DailyRunCap)
	if err != nil {
		return nil, fmt.Errorf("创建自动化规则失败: %w", err)
	}
	id, _ := result.LastInsertId()
	return s.getAutomationRule(ctx, id)
}

func (s *Service) getAutomationRule(ctx context.Context, id int64) (*AutomationRule, error) {
	var rule AutomationRule
	var enabled, dry int
	var triggerRaw, conditionsRaw, actionsRaw string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,enabled,dry_run,trigger_json,conditions_json,actions_json,cooldown_seconds,daily_run_cap,created_at,updated_at FROM automation_rules WHERE id=?`, id).Scan(&rule.ID, &rule.Name, &enabled, &dry, &triggerRaw, &conditionsRaw, &actionsRaw, &rule.CooldownSeconds, &rule.DailyRunCap, &rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		return nil, err
	}
	rule.Enabled, rule.DryRun = enabled == 1, dry == 1
	if err := json.Unmarshal([]byte(triggerRaw), &rule.Trigger); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(conditionsRaw), &rule.Conditions); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(actionsRaw), &rule.Actions); err != nil {
		return nil, err
	}
	return &rule, nil
}

func (s *Service) UpdateAutomationRule(ctx context.Context, rule AutomationRule) error {
	if err := validateAutomationRule(rule); err != nil {
		return err
	}
	trigger, _ := json.Marshal(rule.Trigger)
	conditions, _ := json.Marshal(rule.Conditions)
	actions, _ := json.Marshal(rule.Actions)
	_, err := s.db.ExecContext(ctx, `UPDATE automation_rules SET name=?,enabled=?,dry_run=?,trigger_json=?,conditions_json=?,actions_json=?,cooldown_seconds=?,daily_run_cap=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, rule.Name, boolInt(rule.Enabled), boolInt(rule.DryRun), string(trigger), string(conditions), string(actions), rule.CooldownSeconds, rule.DailyRunCap, rule.ID)
	return err
}
func (s *Service) DeleteAutomationRule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM automation_rules WHERE id=?`, id)
	return err
}

func triggerMatches(trigger FilterNode, event AutomationEvent) bool {
	if strings.EqualFold(trigger.Field, "event_type") || strings.EqualFold(trigger.Field, "event") {
		return compareValue(event.Type, strings.ToLower(trigger.Op), trigger.Value)
	}
	return true
}

func (s *Service) DryRunAutomation(ctx context.Context, ruleID int64, event AutomationEvent) ([]AutomationRun, error) {
	rule, err := s.getAutomationRule(ctx, ruleID)
	if err != nil {
		return nil, err
	}
	if !triggerMatches(rule.Trigger, event) {
		return []AutomationRun{}, nil
	}
	items, err := s.loadDomains(ctx)
	if err != nil {
		return nil, err
	}
	var out []AutomationRun
	now := time.Now().UTC()
	for _, item := range items {
		if event.Domain != "" && !strings.EqualFold(event.Domain, item.Info.Name) {
			continue
		}
		if !filterMatches(rule.Conditions, item) {
			continue
		}
		details := map[string]any{"mode": "dry_run", "actions": rule.Actions, "domain": item.Info.Name}
		data, _ := json.Marshal(details)
		result, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO automation_runs(rule_id,event_id,domain,status,dry_run,action_count,details_json,started_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?)`, rule.ID, event.ID, item.Info.Name, "dry_run", 1, len(rule.Actions), string(data), now, now)
		if err != nil {
			return nil, err
		}
		id, _ := result.LastInsertId()
		out = append(out, AutomationRun{ID: id, RuleID: rule.ID, EventID: event.ID, Domain: item.Info.Name, Status: "dry_run", DryRun: true, ActionCount: len(rule.Actions), Details: details, StartedAt: now, CompletedAt: &now})
	}
	return out, nil
}

func (s *Service) EvaluateAutomation(ctx context.Context, event AutomationEvent, execute bool) ([]AutomationRun, error) {
	if strings.TrimSpace(event.ID) == "" {
		return nil, fmt.Errorf("event_id 不能为空")
	}
	rules, err := s.ListAutomationRules(ctx)
	if err != nil {
		return nil, err
	}
	var out []AutomationRun
	now := time.Now().UTC()
	for _, rule := range rules {
		if !rule.Enabled || !triggerMatches(rule.Trigger, event) {
			continue
		}
		items, err := s.loadDomains(ctx)
		if err != nil {
			return nil, err
		}
		daily := 0
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_runs WHERE rule_id=? AND started_at >= date('now') AND status IN ('dry_run','succeeded')`, rule.ID).Scan(&daily)
		if daily >= rule.DailyRunCap {
			continue
		}
		for _, item := range items {
			if event.Domain != "" && !strings.EqualFold(event.Domain, item.Info.Name) {
				continue
			}
			if !filterMatches(rule.Conditions, item) {
				continue
			}
			var existing int
			_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_runs WHERE rule_id=? AND event_id=? AND lower(domain)=lower(?)`, rule.ID, event.ID, item.Info.Name).Scan(&existing)
			if existing > 0 {
				continue
			}
			if rule.CooldownSeconds > 0 {
				var recent int
				_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_runs WHERE rule_id=? AND lower(domain)=lower(?) AND started_at>?`, rule.ID, item.Info.Name, now.Add(-time.Duration(rule.CooldownSeconds)*time.Second)).Scan(&recent)
				if recent > 0 {
					continue
				}
			}
			dry := rule.DryRun || !execute
			status := "dry_run"
			actionCount := len(rule.Actions)
			details := map[string]any{"actions": rule.Actions, "domain": item.Info.Name}
			if !dry {
				if err := s.applyAutomationActions(ctx, item.Info.Name, rule.Actions); err != nil {
					status = "failed"
					details["error"] = err.Error()
				} else {
					status = "succeeded"
				}
			}
			data, _ := json.Marshal(details)
			result, err := s.db.ExecContext(ctx, `INSERT INTO automation_runs(rule_id,event_id,domain,status,dry_run,action_count,details_json,started_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?)`, rule.ID, event.ID, item.Info.Name, status, boolInt(dry), actionCount, string(data), now, now)
			if err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "unique") {
					continue
				}
				return nil, err
			}
			id, _ := result.LastInsertId()
			out = append(out, AutomationRun{ID: id, RuleID: rule.ID, EventID: event.ID, Domain: item.Info.Name, Status: status, DryRun: dry, ActionCount: actionCount, Details: details, StartedAt: now, CompletedAt: &now})
			daily++
			if daily >= rule.DailyRunCap {
				break
			}
		}
	}
	return out, nil
}

func (s *Service) applyAutomationActions(ctx context.Context, name string, actions []map[string]any) error {
	for _, action := range actions {
		typeName, _ := action["type"].(string)
		switch typeName {
		case "tag":
			tag, _ := action["tag"].(string)
			_, err := s.ExecuteBulk(ctx, BulkAction{Type: "tag", Tag: tag, Domains: []string{name}})
			if err != nil {
				return err
			}
		case "priority":
			value, _ := numberValue(action["priority"])
			p := int(value)
			_, err := s.ExecuteBulk(ctx, BulkAction{Type: "priority", Priority: &p, Domains: []string{name}})
			if err != nil {
				return err
			}
		case "folder":
			var id *int64
			if value, ok := numberValue(action["folder_id"]); ok {
				converted := int64(value)
				id = &converted
			}
			_, err := s.ExecuteBulk(ctx, BulkAction{Type: "folder", FolderID: id, Domains: []string{name}})
			if err != nil {
				return err
			}
		case "notification":
			value, _ := action["enabled"].(bool)
			_, err := s.ExecuteBulk(ctx, BulkAction{Type: "notification", Notify: &value, Domains: []string{name}})
			if err != nil {
				return err
			}
		case "monitor":
			value, _ := action["enabled"].(bool)
			_, err := s.ExecuteBulk(ctx, BulkAction{Type: "monitor", Enabled: &value, Domains: []string{name}})
			if err != nil {
				return err
			}
		case "ai_valuation":
			if _, err := s.ai.Enqueue(ctx, []string{name}); err != nil {
				return err
			}
		case "check", "send_notification": /* 仅记录为安全动作；复用现有 Scheduler/通知服务需由事件适配器调用 */
		}
	}
	return nil
}

func (s *Service) ListAutomationRuns(ctx context.Context, ruleID int64, limit int) ([]AutomationRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT id,rule_id,event_id,domain,status,dry_run,action_count,details_json,started_at,completed_at FROM automation_runs`
	var args []any
	if ruleID > 0 {
		query += ` WHERE rule_id=?`
		args = append(args, ruleID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AutomationRun
	for rows.Next() {
		var run AutomationRun
		var dry int
		var raw string
		var completed sql.NullTime
		if err := rows.Scan(&run.ID, &run.RuleID, &run.EventID, &run.Domain, &run.Status, &dry, &run.ActionCount, &raw, &run.StartedAt, &completed); err != nil {
			return nil, err
		}
		run.DryRun = dry == 1
		_ = json.Unmarshal([]byte(raw), &run.Details)
		if completed.Valid {
			run.CompletedAt = &completed.Time
		}
		out = append(out, run)
	}
	return out, rows.Err()
}
