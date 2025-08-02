package logger

import (
	"io"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

// Logger wraps logrus.Logger with additional functionality
type Logger struct {
	*logrus.Logger
	fields logrus.Fields
}

// Config holds logger configuration
type Config struct {
	Level      string
	Format     string
	Output     string
	File       string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
}

// New creates a new logger instance with the given configuration
func New(config Config) (*Logger, error) {
	logger := logrus.New()

	// Set log level
	level, err := logrus.ParseLevel(config.Level)
	if err != nil {
		return nil, err
	}
	logger.SetLevel(level)

	// Set formatter
	switch config.Format {
	case "json":
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	case "text":
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	default:
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	}

	// Set output
	var output io.Writer
	switch config.Output {
	case "stdout":
		output = os.Stdout
	case "stderr":
		output = os.Stderr
	case "file":
		if config.File == "" {
			config.File = "load-balancer.log"
		}

		// Create directory if it doesn't exist
		dir := filepath.Dir(config.File)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}

		file, err := os.OpenFile(config.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return nil, err
		}
		output = file
	default:
		output = os.Stdout
	}

	logger.SetOutput(output)

	return &Logger{
		Logger: logger,
		fields: make(logrus.Fields),
	}, nil
}

// WithField adds a field to the logger context
func (l *Logger) WithField(key string, value interface{}) *Logger {
	fields := make(logrus.Fields)
	for k, v := range l.fields {
		fields[k] = v
	}
	fields[key] = value

	return &Logger{
		Logger: l.Logger,
		fields: fields,
	}
}

// WithFields adds multiple fields to the logger context
func (l *Logger) WithFields(fields logrus.Fields) *Logger {
	newFields := make(logrus.Fields)
	for k, v := range l.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}

	return &Logger{
		Logger: l.Logger,
		fields: newFields,
	}
}

// WithError adds an error field to the logger context
func (l *Logger) WithError(err error) *Logger {
	return l.WithField("error", err.Error())
}

// Debug logs a debug message
func (l *Logger) Debug(args ...interface{}) {
	l.Logger.WithFields(l.fields).Debug(args...)
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.Logger.WithFields(l.fields).Debugf(format, args...)
}

// Info logs an info message
func (l *Logger) Info(args ...interface{}) {
	l.Logger.WithFields(l.fields).Info(args...)
}

// Infof logs a formatted info message
func (l *Logger) Infof(format string, args ...interface{}) {
	l.Logger.WithFields(l.fields).Infof(format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(args ...interface{}) {
	l.Logger.WithFields(l.fields).Warn(args...)
}

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.Logger.WithFields(l.fields).Warnf(format, args...)
}

// Error logs an error message
func (l *Logger) Error(args ...interface{}) {
	l.Logger.WithFields(l.fields).Error(args...)
}

// Errorf logs a formatted error message
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.Logger.WithFields(l.fields).Errorf(format, args...)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(args ...interface{}) {
	l.Logger.WithFields(l.fields).Fatal(args...)
}

// Fatalf logs a formatted fatal message and exits
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.Logger.WithFields(l.fields).Fatalf(format, args...)
}

// Panic logs a panic message and panics
func (l *Logger) Panic(args ...interface{}) {
	l.Logger.WithFields(l.fields).Panic(args...)
}

// Panicf logs a formatted panic message and panics
func (l *Logger) Panicf(format string, args ...interface{}) {
	l.Logger.WithFields(l.fields).Panicf(format, args...)
}

// RequestLogger creates a logger with request-specific fields
func (l *Logger) RequestLogger(requestID, method, path, remoteAddr string) *Logger {
	return l.WithFields(logrus.Fields{
		"request_id":  requestID,
		"method":      method,
		"path":        path,
		"remote_addr": remoteAddr,
		"component":   "request_handler",
	})
}

// BackendLogger creates a logger with backend-specific fields
func (l *Logger) BackendLogger(backendID, backendURL string) *Logger {
	return l.WithFields(logrus.Fields{
		"backend_id":  backendID,
		"backend_url": backendURL,
		"component":   "backend",
	})
}

// HealthCheckLogger creates a logger with health check specific fields
func (l *Logger) HealthCheckLogger() *Logger {
	return l.WithField("component", "health_check")
}

// LoadBalancerLogger creates a logger with load balancer specific fields
func (l *Logger) LoadBalancerLogger() *Logger {
	return l.WithField("component", "load_balancer")
}

// MetricsLogger creates a logger with metrics specific fields
func (l *Logger) MetricsLogger() *Logger {
	return l.WithField("component", "metrics")
}

// MiddlewareLogger creates a logger with middleware specific fields
func (l *Logger) MiddlewareLogger(middlewareName string) *Logger {
	return l.WithFields(logrus.Fields{
		"component":  "middleware",
		"middleware": middlewareName,
	})
}
