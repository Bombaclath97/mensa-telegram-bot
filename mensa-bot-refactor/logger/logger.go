package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level represents the log level
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelNames = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
}

var levelFromString = map[string]Level{
	"debug": LevelDebug,
	"info":  LevelInfo,
	"warn":  LevelWarn,
	"error": LevelError,
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Caller    string                 `json:"caller,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// Logger is a structured JSON logger
type Logger struct {
	mu     sync.Mutex
	level  Level
	output *os.File
}

var (
	defaultLogger *Logger
	once          sync.Once
)

// Init initializes the default logger with the specified log level
func Init(levelStr string) {
	once.Do(func() {
		level := LevelInfo // default
		if l, ok := levelFromString[strings.ToLower(levelStr)]; ok {
			level = l
		}
		defaultLogger = &Logger{
			level:  level,
			output: os.Stdout,
		}
	})
}

// SetLevel changes the log level
func SetLevel(levelStr string) {
	if defaultLogger == nil {
		Init(levelStr)
		return
	}
	if l, ok := levelFromString[strings.ToLower(levelStr)]; ok {
		defaultLogger.mu.Lock()
		defaultLogger.level = l
		defaultLogger.mu.Unlock()
	}
}

// getLogger returns the default logger, initializing if necessary
func getLogger() *Logger {
	if defaultLogger == nil {
		Init("info")
	}
	return defaultLogger
}

// callerSkip is the stack depth, counted from log's own frame, of the code that actually
// called the logger:
//
//	0  (*Logger).log            — where runtime.Caller is invoked
//	1  Info / Warn / Infof / …  — the exported entry point
//	2  the real call site       ← what we report
//
// Every exported entry point sits exactly one frame above log. That invariant is the whole
// reason the formatted variants (Infof and friends) call log directly instead of delegating
// to Info: delegating adds a frame and mis-attributes the line.
//
// This was 3, which named the caller OF the function containing the log statement. Where
// that frame left application code — a goroutine's top-level function, main, a library's
// handler dispatcher — it escaped into the runtime and reported nonsense sites like
// asm_amd64.s:1771 (runtime.goexit), proc.go:290 (runtime.main) and net/http/server.go.
const callerSkip = 2

// log writes a log entry if the level is enabled
func (l *Logger) log(level Level, msg string, fields map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.level {
		return
	}

	// Get caller information
	_, file, line, ok := runtime.Caller(callerSkip)
	caller := ""
	if ok {
		// Get just the filename, not the full path
		parts := strings.Split(file, "/")
		if len(parts) > 0 {
			caller = fmt.Sprintf("%s:%d", parts[len(parts)-1], line)
		}
	}

	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     levelNames[level],
		Message:   msg,
		Caller:    caller,
		Fields:    fields,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(l.output, `{"timestamp":"%s","level":"ERROR","message":"failed to marshal log entry"}%s`,
			time.Now().UTC().Format(time.RFC3339), "\n")
		return
	}

	fmt.Fprintln(l.output, string(data))
}

// Debug logs a debug message
func Debug(msg string, fields ...map[string]interface{}) {
	var f map[string]interface{}
	if len(fields) > 0 {
		f = fields[0]
	}
	getLogger().log(LevelDebug, msg, f)
}

// Info logs an info message
func Info(msg string, fields ...map[string]interface{}) {
	var f map[string]interface{}
	if len(fields) > 0 {
		f = fields[0]
	}
	getLogger().log(LevelInfo, msg, f)
}

// Warn logs a warning message
func Warn(msg string, fields ...map[string]interface{}) {
	var f map[string]interface{}
	if len(fields) > 0 {
		f = fields[0]
	}
	getLogger().log(LevelWarn, msg, f)
}

// Error logs an error message
func Error(msg string, fields ...map[string]interface{}) {
	var f map[string]interface{}
	if len(fields) > 0 {
		f = fields[0]
	}
	getLogger().log(LevelError, msg, f)
}

// The formatted variants call log DIRECTLY rather than delegating to Debug/Info/Warn/Error.
// Delegating would put them two frames above log instead of one and break callerSkip's
// invariant, reporting this file as the caller of every formatted log line.

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	getLogger().log(LevelDebug, fmt.Sprintf(format, args...), nil)
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	getLogger().log(LevelInfo, fmt.Sprintf(format, args...), nil)
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	getLogger().log(LevelWarn, fmt.Sprintf(format, args...), nil)
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	getLogger().log(LevelError, fmt.Sprintf(format, args...), nil)
}

// WithFields creates a log entry with additional fields
func WithFields(fields map[string]interface{}) *FieldLogger {
	return &FieldLogger{fields: fields}
}

// FieldLogger allows logging with predefined fields
type FieldLogger struct {
	fields map[string]interface{}
}

func (fl *FieldLogger) Debug(msg string) {
	getLogger().log(LevelDebug, msg, fl.fields)
}

func (fl *FieldLogger) Info(msg string) {
	getLogger().log(LevelInfo, msg, fl.fields)
}

func (fl *FieldLogger) Warn(msg string) {
	getLogger().log(LevelWarn, msg, fl.fields)
}

func (fl *FieldLogger) Error(msg string) {
	getLogger().log(LevelError, msg, fl.fields)
}

// Fatal logs an error message and exits
func Fatal(msg string, fields ...map[string]interface{}) {
	var f map[string]interface{}
	if len(fields) > 0 {
		f = fields[0]
	}
	getLogger().log(LevelError, msg, f)
	os.Exit(1)
}

// Fatalf logs a formatted error message and exits. Calls log directly, not Fatal, for the
// same frame-depth reason as Infof and friends.
func Fatalf(format string, args ...interface{}) {
	getLogger().log(LevelError, fmt.Sprintf(format, args...), nil)
	os.Exit(1)
}
