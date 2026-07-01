package logger

import (
	"errors"
	"io"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Options 描述 zap logger 的初始化参数。
type Options struct {
	Environment string
	Service     string
	Output      io.Writer
}

// New 创建 JSON structured logger。
func New(opts Options) (*zap.Logger, error) {
	if opts.Service == "" {
		return nil, errors.New("logger service is required")
	}
	if opts.Environment == "" {
		opts.Environment = "development"
	}
	if opts.Output == nil {
		opts.Output = os.Stdout
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "ts"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(opts.Output),
		zap.InfoLevel,
	)

	return zap.New(core).With(
		zap.String("service", opts.Service),
		zap.String("env", opts.Environment),
	), nil
}

// RedactField 返回已脱敏的 zap 字段，避免调用方误把 secret 写入日志。
func RedactField(key string, _ string) zap.Field {
	return zap.String(key, "[REDACTED]")
}
