package logger

import (
	"testing"
	"time"
)

func TestAsyncConfigOptions(t *testing.T) {
	// Test custom buffer size and flush interval
	logger, err := Init(
		WithLevel("debug"), 
		WithFormat("console"), 
		WithAsync(true),
		WithAsyncBufferSize(1024*1024),  // 1MB buffer
		WithAsyncFlushInterval(10*time.Second), // 10 second flush interval
	)
	if err != nil {
		t.Fatalf("Failed to init async logger with custom config: %v", err)
	}

	// Log a few messages
	for i := 0; i < 5; i++ {
		logger.Info("Async test message with custom config", Int("id", i))
	}

	// Wait to ensure async logs are processed
	time.Sleep(50 * time.Millisecond)
	
	// Sync to flush all buffered logs
	logger.Sync()
	
	t.Log("Async logger with custom buffer size and flush interval test completed successfully")
}

func TestAsyncDefaultConfig(t *testing.T) {
	// Test with default async config (should use 512KB buffer and 30s flush interval)
	logger, err := Init(
		WithLevel("debug"), 
		WithFormat("console"), 
		WithAsync(true),
		// No custom buffer size or flush interval - should use defaults
	)
	if err != nil {
		t.Fatalf("Failed to init async logger with default config: %v", err)
	}

	// Log a few messages
	for i := 0; i < 5; i++ {
		logger.Info("Async test message with default config", Int("id", i))
	}

	// Wait to ensure async logs are processed
	time.Sleep(50 * time.Millisecond)
	
	// Sync to flush all buffered logs
	logger.Sync()
	
	t.Log("Async logger with default config test completed successfully")
}