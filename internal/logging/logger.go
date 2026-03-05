package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync/atomic"
)

// Level defines logging verbosity.
type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

const envLogLevel = "ECHONET_LOG_LEVEL"

var globalLevel atomic.Int32

func init() {
	globalLevel.Store(int32(LevelInfo))
	SetLevelFromEnv()
}

// Logger is a leveled logger for a component.
type Logger struct {
	component string
	out       *log.Logger
	err       *log.Logger
}

// New creates a logger that writes debug/info to stdout
// and warn/error/fatal to stderr.
func New(component string) *Logger {
	return NewWithWriters(component, os.Stdout, os.Stderr)
}

// NewWithWriter creates a logger writing all levels to w.
func NewWithWriter(component string, w io.Writer) *Logger {
	return NewWithWriters(component, w, w)
}

// NewWithWriters creates a logger with separate output streams.
// debug/info go to stdoutWriter; warn/error/fatal go to stderrWriter.
func NewWithWriters(component string, stdoutWriter, stderrWriter io.Writer) *Logger {
	if stdoutWriter == nil {
		stdoutWriter = os.Stdout
	}
	if stderrWriter == nil {
		stderrWriter = os.Stderr
	}
	return &Logger{
		component: component,
		out:       log.New(stdoutWriter, "", log.LstdFlags),
		err:       log.New(stderrWriter, "", log.LstdFlags),
	}
}

// SetLevelFromEnv reads ECHONET_LOG_LEVEL and sets global level.
func SetLevelFromEnv() {
	level := os.Getenv(envLogLevel)
	if err := SetLevel(level); err != nil {
		globalLevel.Store(int32(LevelInfo))
		log.New(os.Stderr, "", log.LstdFlags).Printf("[WARN] [logging] invalid %s=%q; using info", envLogLevel, level)
	}
}

// SetLevel updates global log level.
func SetLevel(level string) error {
	v, err := parseLevel(level)
	if err != nil {
		return err
	}
	globalLevel.Store(int32(v))
	return nil
}

func parseLevel(level string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return LevelInfo, nil
	case "debug":
		return LevelDebug, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("unsupported log level %q", level)
	}
}

func (l *Logger) logf(level Level, label string, format string, args ...any) {
	if level < Level(globalLevel.Load()) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	target := l.out
	if level >= LevelWarn {
		target = l.err
	}
	if l.component != "" {
		target.Printf("[%s] [%s] %s", label, l.component, msg)
		return
	}
	target.Printf("[%s] %s", label, msg)
}

// Debugf logs a debug-level message.
func (l *Logger) Debugf(format string, args ...any) {
	l.logf(LevelDebug, "DEBUG", format, args...)
}

// Infof logs an info-level message.
func (l *Logger) Infof(format string, args ...any) {
	l.logf(LevelInfo, "INFO", format, args...)
}

// Warnf logs a warning-level message.
func (l *Logger) Warnf(format string, args ...any) {
	l.logf(LevelWarn, "WARN", format, args...)
}

// Errorf logs an error-level message.
func (l *Logger) Errorf(format string, args ...any) {
	l.logf(LevelError, "ERROR", format, args...)
}

// Fatalf logs an error-level message and exits.
func (l *Logger) Fatalf(format string, args ...any) {
	l.logf(LevelError, "FATAL", format, args...)
	os.Exit(1)
}
