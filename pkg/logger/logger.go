package logger

import (
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	globalLogger     *zap.SugaredLogger
	globalLoggerOnce sync.Once
)

func getLogger() *zap.SugaredLogger {
	globalLoggerOnce.Do(func() {
		config := zap.NewDevelopmentConfig()
		config.DisableStacktrace = true
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.DateTime)
		// AddCallerSkip(1) để zap báo đúng file:line nơi thực sự gọi log chứ không phải từ logger.go
		logger, _ := config.Build(zap.AddCallerSkip(1))
		defer logger.Sync()
		sugar := logger.Sugar()
		globalLogger = sugar
		zap.ReplaceGlobals(logger)
	})
	return globalLogger
}

func Desugar() *zap.Logger {
	return getLogger().Desugar()
}

func With(args ...any) *zap.SugaredLogger {
	return getLogger().With(args...)
}

func Level() zapcore.Level {
	return getLogger().Level()
}

func Debug(args ...any) {
	getLogger().Debug(args...)
}

func Info(args ...any) {
	getLogger().Info(args...)
}

func Warn(args ...any) {
	getLogger().Warn(args...)
}

func Error(args ...any) {
	getLogger().Error(args...)
}

func DPanic(args ...any) {
	getLogger().DPanic(args...)
}

func Panic(args ...any) {
	getLogger().Panic(args...)
}

func Fatal(args ...any) {
	getLogger().Fatal(args...)
}

func Debugf(template string, args ...any) {
	getLogger().Debugf(template, args...)
}

func Infof(template string, args ...any) {
	getLogger().Infof(template, args...)
}

func Warnf(template string, args ...any) {
	getLogger().Warnf(template, args...)
}

func Errorf(template string, args ...any) {
	getLogger().Errorf(template, args...)
}

func DPanicf(template string, args ...any) {
	getLogger().DPanicf(template, args...)
}

func Panicf(template string, args ...any) {
	getLogger().Panicf(template, args...)
}

func Fatalf(template string, args ...any) {
	getLogger().Fatalf(template, args...)
}

func Debugw(msg string, keysAndValues ...any) {
	getLogger().Debugw(msg, keysAndValues...)
}

func Infow(msg string, keysAndValues ...any) {
	getLogger().Infow(msg, keysAndValues...)
}

func Warnw(msg string, keysAndValues ...any) {
	getLogger().Warnw(msg, keysAndValues...)
}

func Errorw(msg string, keysAndValues ...any) {
	getLogger().Errorw(msg, keysAndValues...)
}

func DPanicw(msg string, keysAndValues ...any) {
	getLogger().DPanicw(msg, keysAndValues...)
}

func Panicw(msg string, keysAndValues ...any) {
	getLogger().Panicw(msg, keysAndValues...)
}

func Fatalw(msg string, keysAndValues ...any) {
	getLogger().Fatalw(msg, keysAndValues...)
}

func Debugln(args ...any) {
	getLogger().Debugln(args...)
}

func Infoln(args ...any) {
	getLogger().Infoln(args...)
}

func Warnln(args ...any) {
	getLogger().Warnln(args...)
}

func Errorln(args ...any) {
	getLogger().Errorln(args...)
}

func DPanicln(args ...any) {
	getLogger().DPanicln(args...)
}

func Panicln(args ...any) {
	getLogger().Panicln(args...)
}

func Fatalln(args ...any) {
	getLogger().Fatalln(args...)
}

func Sync() error {
	return getLogger().Sync()
}
