package core

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/nats-io/nats-server/v2/server"
)

// InitLogger sets up the global slog logger with sane defaults.
func InitLogger() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{AddSource: true, Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))
}

// natsLoggerAdapter implements nats-server Logger interface using slog.
type natsLoggerAdapter struct {
	logger *slog.Logger
}

func NewNATSServerLogger(logger *slog.Logger) server.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return &natsLoggerAdapter{logger: logger}
}

// Logger interface V2 methods
func (nl *natsLoggerAdapter) Noticef(format string, v ...interface{}) {
	// nl.logger.Info(fmt.Sprintf(format, v...))
}
func (nl *natsLoggerAdapter) Warnf(format string, v ...interface{}) {
	nl.logger.Warn(fmt.Sprintf(format, v...))
}
func (nl *natsLoggerAdapter) Errorf(format string, v ...interface{}) {
	nl.logger.Error(fmt.Sprintf(format, v...))
}
func (nl *natsLoggerAdapter) Fatalf(format string, v ...interface{}) {
	nl.logger.Error("NATS FATAL: " + fmt.Sprintf(format, v...))
}
func (nl *natsLoggerAdapter) Debugf(format string, v ...interface{}) {
	nl.logger.Debug(fmt.Sprintf(format, v...))
}
func (nl *natsLoggerAdapter) Tracef(format string, v ...interface{}) {
	nl.logger.Debug("NATS TRACE: " + fmt.Sprintf(format, v...))
}
