// Package logger 提供带统一字段语义的结构化日志。
//
// 约定字段：component / domain / provider / status / latency_ms / count / error。
// 默认输出人类可读的 key=value 行；设置 DOMAINHUNTER_LOG_FORMAT=json 时输出 JSON。
package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Level 日志级别
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Fields 结构化字段
type Fields map[string]any

var (
	mu     sync.RWMutex
	level            = LevelInfo
	out    io.Writer = os.Stdout
	asJSON bool
)

// Init 初始化日志系统。filePath 目前保留用于兼容旧配置，日志统一写标准输出，
// 由容器/systemd 负责收集与轮转。
func Init(levelName, filePath string) error {
	SetLevel(levelName)

	mu.Lock()
	asJSON = strings.EqualFold(strings.TrimSpace(os.Getenv("DOMAINHUNTER_LOG_FORMAT")), "json")
	mu.Unlock()

	if strings.TrimSpace(filePath) != "" {
		Warn("日志文件配置 %s 已忽略：日志统一输出到标准输出，由容器日志驱动负责轮转", filePath)
	}
	return nil
}

// Close 关闭日志系统（保留接口，当前无需释放资源）
func Close() {}

// SetLevel 设置日志级别
func SetLevel(name string) {
	mu.Lock()
	defer mu.Unlock()
	level = parseLevel(name)
}

// SetOutput 替换输出目标（测试用）
func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	out = w
}

func parseLevel(name string) Level {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func enabled(l Level) bool {
	mu.RLock()
	defer mu.RUnlock()
	return l >= level
}

func levelName(l Level) string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

// Logger 绑定了 component 的日志器
type Logger struct {
	component string
	base      Fields
}

// Component 返回绑定组件名的日志器
func Component(name string) *Logger { return &Logger{component: name} }

// With 追加固定字段
func (l *Logger) With(fields Fields) *Logger {
	merged := make(Fields, len(l.base)+len(fields))
	for k, v := range l.base {
		merged[k] = v
	}
	for k, v := range fields {
		merged[k] = v
	}
	return &Logger{component: l.component, base: merged}
}

// Debug 输出调试日志
func (l *Logger) Debug(fields Fields, msg string, args ...any) {
	l.log(LevelDebug, fields, msg, args...)
}

// Info 输出信息日志
func (l *Logger) Info(fields Fields, msg string, args ...any) { l.log(LevelInfo, fields, msg, args...) }

// Warn 输出警告日志
func (l *Logger) Warn(fields Fields, msg string, args ...any) { l.log(LevelWarn, fields, msg, args...) }

// Error 输出错误日志
func (l *Logger) Error(fields Fields, msg string, args ...any) {
	l.log(LevelError, fields, msg, args...)
}

func (l *Logger) log(lv Level, fields Fields, msg string, args ...any) {
	if !enabled(lv) {
		return
	}
	merged := make(Fields, len(l.base)+len(fields)+1)
	for k, v := range l.base {
		merged[k] = v
	}
	for k, v := range fields {
		merged[k] = v
	}
	if l.component != "" {
		merged["component"] = l.component
	}
	write(lv, fmt.Sprintf(msg, args...), merged)
}

func write(lv Level, msg string, fields Fields) {
	mu.RLock()
	w := out
	useJSON := asJSON
	mu.RUnlock()

	if useJSON {
		payload := make(map[string]any, len(fields)+3)
		for k, v := range fields {
			payload[k] = v
		}
		payload["level"] = strings.ToLower(levelName(lv))
		payload["msg"] = msg
		payload["time"] = time.Now().Format(time.RFC3339)
		if encoded, err := json.Marshal(payload); err == nil {
			fmt.Fprintln(w, string(encoded))
			return
		}
	}

	var b strings.Builder
	b.WriteString(time.Now().Format("2006-01-02 15:04:05"))
	b.WriteString(" ")
	b.WriteString(levelName(lv))
	if component, ok := fields["component"].(string); ok && component != "" {
		b.WriteString(" [")
		b.WriteString(component)
		b.WriteString("]")
	}
	b.WriteString(" ")
	b.WriteString(msg)

	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k != "component" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%v", k, fields[k])
	}
	fmt.Fprintln(w, b.String())
}

// 以下为包级快捷方法，保持与重构前一致的调用方式。

// Debug 输出调试日志
func Debug(format string, args ...any) { write3(LevelDebug, format, args...) }

// Info 输出信息日志
func Info(format string, args ...any) { write3(LevelInfo, format, args...) }

// Warn 输出警告日志
func Warn(format string, args ...any) { write3(LevelWarn, format, args...) }

// Error 输出错误日志
func Error(format string, args ...any) { write3(LevelError, format, args...) }

// Fatal 输出致命错误并退出进程
func Fatal(format string, args ...any) {
	write(LevelError, fmt.Sprintf(format, args...), Fields{})
	os.Exit(1)
}

func write3(lv Level, format string, args ...any) {
	if !enabled(lv) {
		return
	}
	write(lv, fmt.Sprintf(format, args...), Fields{})
}
