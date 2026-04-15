package stat

import (
	"github.com/18721889353/sunshine/pkg/logger"
	"testing"
	"time"
)

func TestInit(t *testing.T) {
	Init(
		// test empty
		WithPrintInterval(0),

		WithPrintInterval(time.Second),
		WithPrintField(logger.String("host", "127.0.0.1")),

		WithAlarm(WithCPUThreshold(0.9), WithMemoryThreshold(0.85)),
	)

	time.Sleep(time.Second * 2)
}
