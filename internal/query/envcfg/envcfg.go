// Package envcfg 处理查询源的环境变量读取。
//
// 兼容性约定：新的 DOMAINHUNTER_* 变量优先，旧的 PUFF_* 变量继续有效（已废弃，
// 但不能删除，否则现有部署滚动升级时会丢配置）。
package envcfg

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// First 返回第一个非空的环境变量值（primary 优先于 legacy）
func First(primary, legacy string) string {
	if value := strings.TrimSpace(os.Getenv(primary)); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(legacy))
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
