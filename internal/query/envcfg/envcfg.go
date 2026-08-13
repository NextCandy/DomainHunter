// Package envcfg 处理查询源的 DomainHunter 环境变量读取。
package envcfg

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Value 返回环境变量的去空格值。
func Value(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

// ParseTLDs 解析逗号分隔的 TLD 列表；为空时使用 defaults
func ParseTLDs(raw string, defaults ...string) map[string]struct{} {
	items := strings.Split(raw, ",")
	if strings.TrimSpace(raw) == "" {
		items = defaults
	}
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		tld := strings.ToLower(strings.Trim(strings.TrimSpace(item), "."))
		if tld != "" {
			result[tld] = struct{}{}
		}
	}
	return result
}

// ParseDuration 解析超时配置，支持 "20"（秒）与 "20s" 两种写法，上限 120 秒
func ParseDuration(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		d := time.Duration(seconds) * time.Second
		if d > 0 && d <= 120*time.Second {
			return d, true
		}
		return 0, false
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 && d <= 120*time.Second {
		return d, true
	}
	return 0, false
}
