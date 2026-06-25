package gows

import (
	"testing"
)

// ---------------------------------------------------------------------------
// TestWriteQueueFullError
// ---------------------------------------------------------------------------

func TestWriteQueueFullError(t *testing.T) {
	t.Parallel()

	err := ErrWriteQueueFull
	if err.Error() == "" {
		t.Error("ErrWriteQueueFull should not have empty error string")
	}

	// 验证它是 ErrWriteQueueFull
	if err != ErrWriteQueueFull {
		t.Errorf("expected ErrWriteQueueFull, got %T", err)
	}
}
