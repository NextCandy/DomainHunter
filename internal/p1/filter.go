package p1

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const filterVersion = 1

var allowedFilterFields = map[string]bool{
	"name": true, "status": true, "tld": true, "registrar": true, "provider": true,
	"tag": true, "folder_id": true, "favorite": true, "enabled": true,
	"created_at": true, "last_checked": true, "expiry_at": true,
	"ai_quality": true, "ai_liquidity": true, "ai_risk": true,
	"ai_value_low": true, "ai_value_high": true, "ai_confidence": true,
	"event": true, "event_type": true,
}

func ParseFilter(raw string) (FilterNode, error) {
	if strings.TrimSpace(raw) == "" {
		return FilterNode{Version: filterVersion, Logic: "and"}, nil
	}
	if len(raw) > 64*1024 {
		return FilterNode{}, fmt.Errorf("筛选条件不能超过64KB")
	}
	var node FilterNode
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return FilterNode{}, fmt.Errorf("筛选条件不是合法 JSON")
	}
	if node.Version == 0 {
		node.Version = filterVersion
	}
	if node.Version != filterVersion {
		return FilterNode{}, fmt.Errorf("不支持的筛选版本: %d", node.Version)
	}
	if err := validateFilter(node, 0); err != nil {
		return FilterNode{}, err
	}
	return node, nil
}

func validateFilter(node FilterNode, depth int) error {
	if depth > 6 {
		return fmt.Errorf("筛选条件嵌套不能超过6层")
	}
	logic := strings.ToLower(strings.TrimSpace(node.Logic))
	if logic != "" && logic != "and" && logic != "or" {
		return fmt.Errorf("不支持的条件逻辑: %s", logic)
	}
	if len(node.Conditions) > 100 {
		return fmt.Errorf("单个筛选组最多100个条件")
	}
	if node.Field != "" {
		field := strings.ToLower(strings.TrimSpace(node.Field))
		if !allowedFilterFields[field] {
			return fmt.Errorf("不支持的筛选字段: %s", field)
		}
		if !allowedOps[strings.ToLower(strings.TrimSpace(node.Op))] {
			return fmt.Errorf("不支持的筛选操作: %s", node.Op)
		}
	}
	for _, child := range node.Conditions {
		if err := validateFilter(child, depth+1); err != nil {
			return err
		}
	}
	if node.Field == "" && len(node.Conditions) == 0 && depth == 0 {
		return nil
	}
	if node.Field == "" && len(node.Conditions) == 0 {
		return fmt.Errorf("筛选组不能为空")
	}
	return nil
}

var allowedOps = map[string]bool{
	"eq": true, "neq": true, "in": true, "not_in": true, "contains": true,
	"starts_with": true, "gte": true, "lte": true, "gt": true, "lt": true,
	"is_empty": true, "not_empty": true,
}

func filterMatches(node FilterNode, item richDomain) bool {
	if node.Field != "" {
		return conditionMatches(strings.ToLower(strings.TrimSpace(node.Field)), strings.ToLower(strings.TrimSpace(node.Op)), node.Value, item)
	}
	if len(node.Conditions) == 0 {
		return true
	}
	logic := strings.ToLower(strings.TrimSpace(node.Logic))
	if logic == "or" {
		for _, child := range node.Conditions {
			if filterMatches(child, item) {
				return true
			}
		}
		return false
	}
	for _, child := range node.Conditions {
		if !filterMatches(child, item) {
			return false
		}
	}
	return true
}

func conditionMatches(field, op string, raw any, item richDomain) bool {
	value, ok := item.field(field)
	if !ok {
		return false
	}
	if op == "is_empty" || op == "not_empty" {
		empty := isEmptyValue(value)
		return (op == "is_empty" && empty) || (op == "not_empty" && !empty)
	}
	if op == "in" || op == "not_in" {
		values, ok := raw.([]any)
		if !ok {
			return false
		}
		matched := false
		for _, candidate := range values {
			if compareValue(value, "eq", candidate) {
				matched = true
				break
			}
		}
		return (op == "in" && matched) || (op == "not_in" && !matched)
	}
	return compareValue(value, op, raw)
}

func isEmptyValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []string:
		return len(typed) == 0
	case *time.Time:
		return typed == nil
	default:
		return false
	}
}

func compareValue(left any, op string, right any) bool {
	if leftTime, ok := left.(*time.Time); ok {
		rightTime, ok := parseTimeValue(right)
		if !ok || leftTime == nil {
			return false
		}
		switch op {
		case "eq":
			return leftTime.Equal(rightTime)
		case "neq":
			return !leftTime.Equal(rightTime)
		case "gte":
			return !leftTime.Before(rightTime)
		case "lte":
			return !leftTime.After(rightTime)
		case "gt":
			return leftTime.After(rightTime)
		case "lt":
			return leftTime.Before(rightTime)
		}
		return false
	}
	if leftNumber, ok := numberValue(left); ok {
		if rightNumber, ok := numberValue(right); ok {
			switch op {
			case "eq":
				return leftNumber == rightNumber
			case "neq":
				return leftNumber != rightNumber
			case "gte":
				return leftNumber >= rightNumber
			case "lte":
				return leftNumber <= rightNumber
			case "gt":
				return leftNumber > rightNumber
			case "lt":
				return leftNumber < rightNumber
			}
		}
	}
	leftString := strings.ToLower(strings.TrimSpace(fmt.Sprint(left)))
	rightString := strings.ToLower(strings.TrimSpace(fmt.Sprint(right)))
	if values, ok := left.([]string); ok {
		for _, candidate := range values {
			if compareValue(candidate, op, right) {
				return true
			}
		}
		return false
	}
	switch op {
	case "eq":
		return leftString == rightString
	case "neq":
		return leftString != rightString
	case "contains":
		return strings.Contains(leftString, rightString)
	case "starts_with":
		return strings.HasPrefix(leftString, rightString)
	default:
		return false
	}
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func parseTimeValue(value any) (time.Time, bool) {
	text := strings.TrimSpace(fmt.Sprint(value))
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func filterURL(node FilterNode) string {
	encoded, _ := json.Marshal(node)
	return url.QueryEscape(string(encoded))
}

func decodeJSONValue(raw json.RawMessage) any {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

func stringListValue(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return out
	case string:
		return []string{typed}
	default:
		return nil
	}
}
