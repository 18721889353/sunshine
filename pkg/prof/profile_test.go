package prof

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProfile_DefaultOptions(t *testing.T) {
	p := NewProfile()
	require.NotNil(t, p)
	assert.NotNil(t, p.stopCh)
	assert.Empty(t, p.Files())
	assert.Equal(t, uint32(DefaultDuration), p.durationSec)
	assert.False(t, p.traceOn)
	assert.Contains(t, p.outputDir, getServerName()+"_profile")
}

func TestNewProfile_WithOptions(t *testing.T) {
	tempDir := t.TempDir()
	errCount := 0

	p := NewProfile(
		WithProfileDuration(3),
		WithProfileTrace(true),
		WithProfileOutputDir(tempDir),
		WithProfileErrorHandler(func(err error) {
			errCount++
			t.Logf("error handler called: %v", err)
		}),
	)

	require.NotNil(t, p)
	assert.Equal(t, uint32(3), p.durationSec)
	assert.True(t, p.traceOn)
	assert.Equal(t, tempDir, p.outputDir)

	// Start sampling, then stop after 1 second
	p.StartOrStop()
	time.Sleep(time.Second)
	p.StartOrStop()

	// Verify output files exist
	files := p.Files()
	assert.NotEmpty(t, files, "should have produced profile files")
	for _, f := range files {
		assert.FileExists(t, f, "profile file should exist: %s", f)
		t.Logf("profile file: %s", f)
		assert.Contains(t, f, getServerName())
	}

	// Test Cleanup
	p.Cleanup()
	assert.Empty(t, p.Files())
	for _, f := range files {
		_, err := os.Stat(f)
		assert.True(t, os.IsNotExist(err), "file should be deleted: %s", f)
	}
}

func TestProfile_StartOrStop(t *testing.T) {
	tempDir := t.TempDir()

	p := NewProfile(
		WithProfileDuration(10),
		WithProfileOutputDir(tempDir),
	)
	require.NotNil(t, p)

	// Start
	p.StartOrStop()
	time.Sleep(500 * time.Millisecond)

	// Stop
	p.StartOrStop()

	files := p.Files()
	assert.NotEmpty(t, files, "should have profile files after stop")

	// Verify files are in the correct directory
	for _, f := range files {
		assert.Equal(t, tempDir, filepath.Dir(f), "file should be in custom output dir")
	}

	// Start again (should work after stop)
	assert.True(t, len(p.closeFns) == 0, "closeFns should be reset after stop")

	// Verify status is back to stopped
	assert.Equal(t, uint32(0), atomic.LoadUint32(&status))
}

func TestProfile_AutoTimeout(t *testing.T) {
	tempDir := t.TempDir()

	p := NewProfile(
		WithProfileDuration(1), // 1 second auto timeout
		WithProfileOutputDir(tempDir),
	)
	require.NotNil(t, p)

	// Start sampling (should auto-stop after 1 second)
	p.StartOrStop()
	time.Sleep(2500 * time.Millisecond)

	files := p.Files()
	assert.NotEmpty(t, files, "auto-stopped profile should have files")
	assert.Equal(t, uint32(0), atomic.LoadUint32(&status), "status should be stopped after timeout")
}

func TestProfile_EnableTrace(t *testing.T) {
	tempDir := t.TempDir()

	p := NewProfile(
		WithProfileDuration(2),
		WithProfileTrace(true),
		WithProfileOutputDir(tempDir),
	)
	require.NotNil(t, p)
	assert.True(t, p.traceOn)

	p.StartOrStop()
	time.Sleep(time.Second)
	p.StartOrStop()

	files := p.Files()
	assert.NotEmpty(t, files)

	// Verify we have the expected type files
	assert.GreaterOrEqual(t, len(files), 6, "should have at least 6 profile types (cpu+mem+goroutine+block+mutex+threadcreate)")
}

func TestSetDurationSecond(t *testing.T) {
	SetDurationSecond(30)
	defer SetDurationSecond(DefaultDuration)

	p := NewProfile()
	assert.Equal(t, uint32(30), p.durationSec)
}

func TestEnableTrace(t *testing.T) {
	EnableTrace()
	defer func() { defaultTraceOn.Store(false) }()

	p := NewProfile()
	assert.True(t, p.traceOn)
}

func TestProfile_NilSafety(t *testing.T) {
	var p *Profile = nil

	// These should not panic
	p.StartOrStop()
	assert.Nil(t, p.Files())
	p.Cleanup()
}

func TestProfile_OutputFiles(t *testing.T) {
	tempDir := t.TempDir()

	p := NewProfile(
		WithProfileDuration(2),
		WithProfileOutputDir(tempDir),
	)
	require.NotNil(t, p)

	// Before sampling
	assert.Empty(t, p.Files())

	// Start and stop
	p.StartOrStop()
	time.Sleep(500 * time.Millisecond)
	p.StartOrStop()

	files := p.Files()
	assert.NotEmpty(t, files)

	// Files() should return a read-only copy (not modifiable by caller)
	origFiles := p.Files()
	origLen := len(origFiles)
	origFiles = append(origFiles, "extra")
	// Internal state should be unchanged
	assert.Equal(t, origLen, len(p.Files()), "internal files should not be affected by modifying returned slice")
}

func TestProfile_FilePath(t *testing.T) {
	tempDir := t.TempDir()
	p := NewProfile(
		WithProfileDuration(1),
		WithProfileOutputDir(tempDir),
	)
	require.NotNil(t, p)

	filePath := p.getFilePath(profileTypeCPU)
	assert.Contains(t, filePath, "cpu.out")
	assert.Contains(t, filePath, getServerName())
	assert.Contains(t, filePath, tempDir)

	// Unknown type
	filePath = p.getFilePath(ProfileType(999))
	assert.Contains(t, filePath, "unknown.out")
}

func TestProfile_MultipleStartCalls(t *testing.T) {
	tempDir := t.TempDir()

	p := NewProfile(
		WithProfileDuration(2),
		WithProfileOutputDir(tempDir),
	)

	// First start
	p.StartOrStop()
	time.Sleep(200 * time.Millisecond)

	// Call Stop twice - second call should be no-op
	p.StartOrStop()

	firstFiles := p.Files()
	assert.NotEmpty(t, firstFiles)

	// Second start
	p.StartOrStop()
	time.Sleep(200 * time.Millisecond)
	p.StartOrStop()

	secondFiles := p.Files()
	assert.NotEmpty(t, secondFiles)
}
