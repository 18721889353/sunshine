package logger

import (
	"testing"
	"time"
)

func TestAsyncLogger(t *testing.T) {
	// Test async logger initialization
	logger, err := Init(WithLevel("debug"), WithFormat("console"), WithAsync(true))
	if err != nil {
		t.Fatalf("Failed to init async logger: %v", err)
	}

	// Log a few messages
	for i := 0; i < 10; i++ {
		logger.Info("Async test message", Int("id", i))
	}

	// Wait to ensure async logs are processed
	time.Sleep(50 * time.Millisecond)
	
	// Sync to flush all buffered logs
	logger.Sync()
	
	t.Log("Async logger test completed successfully")
}

func TestSyncLogger(t *testing.T) {
	// Test sync logger initialization (default behavior)
	logger, err := Init(WithLevel("debug"), WithFormat("console"), WithAsync(false))
	if err != nil {
		t.Fatalf("Failed to init sync logger: %v", err)
	}

	// Log a few messages
	for i := 0; i < 10; i++ {
		logger.Info("Sync test message", Int("id", i))
	}

	t.Log("Sync logger test completed successfully")
}