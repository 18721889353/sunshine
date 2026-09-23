package logger

import (
	"fmt"

	"go.uber.org/zap"
	"google.golang.org/grpc/grpclog"
)

type grpcLogger struct {
	zLog      *zap.Logger
	verbosity int
}

// ReplaceGRPCLoggerV2 替换 gRPC 默认日志记录器为 zap 实现
func ReplaceGRPCLoggerV2(l *zap.Logger) {
	zLog := l.WithOptions(zap.AddCallerSkip(5)).With(zap.Bool("grpc_system", true))
	zzl := &grpcLogger{
		zLog:      zLog,
		verbosity: 0,
	}
	grpclog.SetLoggerV2(zzl)
}

func (l *grpcLogger) Info(args ...interface{}) {
	if !l.V(0) {
		return
	}
	l.zLog.Info(fmt.Sprint(args...))
}

func (l *grpcLogger) Infoln(args ...interface{}) {
	if !l.V(0) {
		return
	}
	l.zLog.Info(fmt.Sprint(args...))
}

func (l *grpcLogger) Infof(format string, args ...interface{}) {
	if !l.V(0) {
		return
	}
	l.zLog.Info(fmt.Sprintf(format, args...))
}

func (l *grpcLogger) Warning(args ...interface{}) {
	if !l.V(0) {
		return
	}
	l.zLog.Warn(fmt.Sprint(args...))
}

func (l *grpcLogger) Warningln(args ...interface{}) {
	if !l.V(0) {
		return
	}
	l.zLog.Warn(fmt.Sprint(args...))
}

func (l *grpcLogger) Warningf(format string, args ...interface{}) {
	if !l.V(0) {
		return
	}
	l.zLog.Warn(fmt.Sprintf(format, args...))
}

func (l *grpcLogger) Error(args ...interface{}) {
	if !l.V(0) {
		return
	}
	l.zLog.Error(fmt.Sprint(args...))
}

func (l *grpcLogger) Errorln(args ...interface{}) {
	if !l.V(0) {
		return
	}
	l.zLog.Error(fmt.Sprint(args...))
}

func (l *grpcLogger) Errorf(format string, args ...interface{}) {
	if !l.V(0) {
		return
	}
	l.zLog.Error(fmt.Sprintf(format, args...))
}

func (l *grpcLogger) Fatal(args ...interface{}) {
	l.zLog.Fatal(fmt.Sprint(args...))
}

func (l *grpcLogger) Fatalln(args ...interface{}) {
	l.zLog.Fatal(fmt.Sprint(args...))
}

func (l *grpcLogger) Fatalf(format string, args ...interface{}) {
	l.zLog.Fatal(fmt.Sprintf(format, args...))
}

func (l *grpcLogger) V(level int) bool {
	return l.verbosity <= level
}
