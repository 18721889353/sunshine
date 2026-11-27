package mysql

import (
	"testing"
	"time"

	glogger "gorm.io/gorm/logger"
)

// TestSmoke_DefaultOptions 默认值合理（无 DB 连接即可验证）。
func TestSmoke_DefaultOptions(t *testing.T) {
	o := defaultOptions()
	if o.maxIdleConns <= 0 || o.maxOpenConns <= 0 {
		t.Errorf("连接池默认值应 >0: idle=%d open=%d", o.maxIdleConns, o.maxOpenConns)
	}
	if o.connMaxLifetime <= 0 || o.maxIdleTime <= 0 {
		t.Errorf("连接生命周期默认值应 >0: lifetime=%v idletime=%v", o.connMaxLifetime, o.maxIdleTime)
	}
	if !o.disableForeignKey {
		t.Error("生产默认应禁用外键")
	}
}

// TestSmoke_Options 各 Option 覆盖字段。
func TestSmoke_Options(t *testing.T) {
	o := defaultOptions()
	for _, opt := range []Option{
		WithLogging(glogger.Warn),
		WithSlowThreshold(200 * time.Millisecond),
		WithMaxIdleConns(7),
		WithMaxOpenConns(70),
		WithConnMaxLifetime(15 * time.Minute),
		WithMaxIdleTime(4 * time.Minute),
	} {
		opt(o)
	}
	if !o.isLog || o.logLevel != glogger.Warn {
		t.Errorf("WithLogging 未生效: isLog=%v level=%v", o.isLog, o.logLevel)
	}
	if o.slowThreshold != 200*time.Millisecond {
		t.Errorf("slowThreshold 未生效, got %v", o.slowThreshold)
	}
	if o.maxIdleConns != 7 || o.maxOpenConns != 70 {
		t.Errorf("连接池选项未生效: idle=%d open=%d", o.maxIdleConns, o.maxOpenConns)
	}
	if o.connMaxLifetime != 15*time.Minute || o.maxIdleTime != 4*time.Minute {
		t.Errorf("生命周期选项未生效: %v / %v", o.connMaxLifetime, o.maxIdleTime)
	}
}
