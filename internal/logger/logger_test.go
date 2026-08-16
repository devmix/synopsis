package logger_test

import (
	"testing"

	"github.com/devmix/synopsis/internal/logger"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		level   string
		wantErr bool
	}{
		{name: "info level", level: "info", wantErr: false},
		{name: "debug level", level: "debug", wantErr: false},
		{name: "warn level", level: "warn", wantErr: false},
		{name: "error level", level: "error", wantErr: false},
		{name: "invalid level", level: "trace", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log, err := logger.New(logger.Options{Level: tt.level})
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if log != nil {
				_ = log.Sync()
			}
		})
	}
}

func TestLoggerMethods(t *testing.T) {
	t.Parallel()

	log, err := logger.New(logger.Options{Level: "debug"})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer log.Sync() //nolint:errcheck

	log.Info("test info message")
	log.Debug("test debug message", "key", "value")
	log.Warn("test warn message")
	log.Error("test error message", logger.Err(nil))
}
