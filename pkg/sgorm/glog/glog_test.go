package glog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/stretchr/testify/assert"
	gormLogger "gorm.io/gorm/logger"
)

func TestNewCustomGormLogger(t *testing.T) {
	l := NewCustomGormLogger("request_id", gormLogger.Info)

	l.LogMode(gormLogger.Info)
	ctx := context.WithValue(context.Background(), "request_id", "123")
	l.Info(ctx, "info", "foo")
	l.Warn(ctx, "warn", "bar")
	l.Error(ctx, "error", "foo bar")

	l.LogMode(gormLogger.Silent)
	l.Trace(ctx, time.Now(), nil, nil)

	l.LogMode(gormLogger.Info)
	l.Trace(ctx, time.Now(), func() (string, int64) {
		return "sql statement", 1
	}, nil)
	l.Trace(ctx, time.Now(), func() (string, int64) {
		return "sql statement", -1
	}, nil)

	l.Trace(ctx, time.Now(), func() (string, int64) {
		return "sql statement", 0
	}, gormLogger.ErrRecordNotFound)

	l.Trace(ctx, time.Now(), func() (string, int64) {
		return "sql statement", 0
	}, errors.New("Error 1054: Unknown column 'test_column'"))

	l.LogMode(gormLogger.Warn)
	l.Trace(ctx, time.Now(), func() (string, int64) {
		return "sql statement", 0
	}, gormLogger.ErrRecordNotFound)
}

func Test_requestIDField(t *testing.T) {
	ctx := context.WithValue(context.Background(), "request_id", "123")
	field := requestIDField(ctx, "")
	assert.Equal(t, logger.Skip(), field)
	field = requestIDField(ctx, "your request id key")
	assert.Equal(t, logger.Skip(), field)
	field = requestIDField(ctx, "request_id")
	assert.Equal(t, logger.String("request_id", "123"), field)
}
