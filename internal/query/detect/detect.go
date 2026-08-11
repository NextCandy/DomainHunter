// Package detect 汇总跨 Provider 共用的文本语义判定。
//
// 这些判定直接决定"能不能报告可注册"，因此集中在一处，避免 RDAP、WHOIS 与
// 备用服务各自维护一份逐渐分叉的词表。
package detect

import (
	"strings"

	"DomainHunter/internal/registry"
)

// builtinReservedMarkers 是即使配置读取失败也必须生效的最小安全词表。
// 命中其中任何一条都意味着"注册局明确说了不能注册"，绝不是可注册。
var builtinReservedMarkers = []string{
	"cannot be registered",
	"prohibited string",
	"registration prohibited",
	"not available for registration",
	"not available for second level registration",
	"reserved domain",
	"domain is reserved",
	"registry policy",
	"domain is not allowed",
}

// notFoundMarkers 是可以判定"域名不存在"的明确语义。
var notFoundMarkers = []string{
	"domain not found",
	"no matching record",
	"no match",
	"object does not exist",
	"not registered",
	"not found",
}

// ContainsReserved 判断文本是否包含保留域名 / 禁止注册 / 注册局策略拒绝语义
func ContainsReserved(raw string, fields ...string) bool {
	text := lowerJoin(raw, fields)
	markers := append([]string{}, registry.Patterns().ReservedPatterns...)
	markers = append(markers, builtinReservedMarkers...)
	for _, marker := range markers {
		marker = strings.ToLower(strings.TrimSpace(marker))
		if marker != "" && strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// ContainsNotFound 判断文本是否包含明确的"域名不存在"语义
func ContainsNotFound(raw string, fields ...string) bool {
	text := lowerJoin(raw, fields)
	for _, marker := range notFoundMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func lowerJoin(raw string, fields []string) string {
	if len(fields) == 0 {
		return strings.ToLower(raw)
	}
	return strings.ToLower(raw + " " + strings.Join(fields, " "))
}
