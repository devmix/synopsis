// Package logger provides structured logging using uber-go/zap.
package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Options configures the logger instance.
type Options struct {
	Level string // zap level: "debug", "info", "warn", "error"
}

// Logger wraps zap.SugaredLogger for convenient structured logging.
type Logger struct {
	sugar *zap.SugaredLogger
	base  *zap.Logger
}

// New creates a new logger with the given options.
func New(opts Options) (*Logger, error) {
	level := zap.InfoLevel
	if err := level.UnmarshalText([]byte(opts.Level)); err != nil {
		return nil, fmt.Errorf("parse log level %q: %w", opts.Level, err)
	}

	encCfg := zap.NewDevelopmentEncoderConfig()
	encCfg.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
	}
	encCfg.EncodeLevel = zapcore.CapitalLevelEncoder
	encCfg.EncodeCaller = func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
		stem := filepath.Base(caller.File)
		stem = stem[:len(stem)-len(filepath.Ext(stem))]
		enc.AppendString("[" + stem + "]")
	}

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encCfg),
		zapcore.AddSync(os.Stdout),
		zap.NewAtomicLevelAt(level),
	)

	base := zap.New(core,
		zap.AddCaller(),
		zap.AddCallerSkip(1),
		zap.AddStacktrace(zap.ErrorLevel),
	)

	return &Logger{
		sugar: base.Sugar(),
		base:  base,
	}, nil
}

// Sync flushes any buffered log entries. Callers should defer this before exit.
func (l *Logger) Sync() error {
	return l.base.Sync()
}

// Info logs an informational message.
func (l *Logger) Info(msg string) {
	l.sugar.Info(msg)
}

// Infow logs an informational message with structured fields.
func (l *Logger) Infow(msg string, fields ...any) {
	l.sugar.Infow(msg, fields...)
}

// Debug logs a debug-level message.
func (l *Logger) Debug(msg string, fields ...any) {
	l.sugar.Debugw(msg, fields...)
}

// Warn logs a warning message.
func (l *Logger) Warn(msg string, fields ...any) {
	l.sugar.Warnw(msg, fields...)
}

// Error logs an error message with optional structured fields.
func (l *Logger) Error(msg string, fields ...any) {
	l.sugar.Errorw(msg, fields...)
}

// Fatal logs a fatal message and calls os.Exit(1).
func (l *Logger) Fatal(msg string, fields ...any) {
	l.sugar.Fatalw(msg, fields...)
}

// Err is a helper to attach an error field to log entries.
func Err(err error) any {
	return zap.Error(err)
}
