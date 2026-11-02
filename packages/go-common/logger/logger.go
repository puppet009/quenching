// Package logger 提供带 trace_id 的结构化日志。
// 输出目标为 stdout（供 kubectl logs 查看）；调用 InitFile 可同时写入
// 按服务/本地日期分目录的文件，底层目录由部署挂载到宿主机 logs 或 PVC。
package logger

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ctxKey struct{}

var std *zap.Logger

// FileConfig 日志文件落盘与轮转配置。Dir 为空表示不落盘。
type FileConfig struct {
	Dir        string // 日志目录
	Name       string // 文件名（不含目录）
	MaxSizeMB  int    // 单文件最大体积（MB）
	MaxBackups int    // 保留旧文件个数
	MaxAgeDays int    // 保留天数
}

func buildLogger(level string, fc FileConfig) *zap.Logger {
	lvl := zapcore.InfoLevel
	_ = lvl.UnmarshalText([]byte(level))

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	encoder := zapcore.NewJSONEncoder(encCfg)

	cores := []zapcore.Core{
		zapcore.NewCore(encoder, zapcore.Lock(os.Stdout), lvl),
	}
	if fc.Dir != "" {
		if err := os.MkdirAll(fc.Dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "创建日志目录 %s 失败，仅输出到 stdout: %v\n", fc.Dir, err)
		} else {
			if fc.Name == "" {
				fc.Name = "app.log"
			}
			if fc.MaxSizeMB <= 0 {
				fc.MaxSizeMB = 100
			}
			writer := &dailyWriter{config: fc, now: time.Now, instance: fileInstance(os.Getenv("POD_NAME"))}
			cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(writer), lvl))
		}
	}
	return zap.New(zapcore.NewTee(cores...), zap.AddCaller())
}

// NewFile creates an independent sink without changing standard/global logging.
func NewFile(level string, fc FileConfig) *zap.Logger {
	return buildLogger(level, fc)
}

// Init 初始化全局 logger（仅 stdout）。level: debug/info/warn/error。
func Init(level string) *zap.Logger {
	return InitFile(level, FileConfig{})
}

// InitFile 初始化全局 logger：stdout 与文件同时输出，文件按大小/天数自动轮转。
func InitFile(level string, fc FileConfig) *zap.Logger {
	std = buildLogger(level, fc)
	// Existing services and background workers use the standard log package too.
	// Route it through the same JSON encoder and file sink as request logs.
	zap.RedirectStdLog(std)
	return std
}

// InitWithFile 以默认轮转参数初始化（单文件 100MB、保留 7 个、30 天）。
// dir 为空时仅输出 stdout，本地开发无感；dir 非空时同时落盘。
func InitWithFile(level, dir, name string) *zap.Logger {
	return InitFile(level, FileConfig{
		Dir:        dir,
		Name:       name,
		MaxSizeMB:  100,
		MaxBackups: 7,
		MaxAgeDays: 30,
	})
}

// L 返回全局 logger。
func L() *zap.Logger {
	if std == nil {
		std, _ = zap.NewProduction()
	}
	return std
}

// WithTrace 把 trace_id 放入 context。
func WithTrace(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, ctxKey{}, traceID)
}

// From 从 context 取带 trace_id 字段的 logger。
func From(ctx context.Context) *zap.Logger {
	l := L()
	if v := ctx.Value(ctxKey{}); v != nil {
		if s, ok := v.(string); ok && s != "" {
			l = l.With(zap.String("trace_id", s))
		}
	}
	return l
}
