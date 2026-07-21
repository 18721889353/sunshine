package initial

import (
	"context"
	"time"

	"github.com/18721889353/sunshine/pkg/app"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/tracer"

	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/internal/database"
)

// Close releasing resources after service exit
func Close(servers []app.IServer) []app.Close {
	var closes []app.Close

	// close server
	for _, s := range servers {
		closes = append(closes, s.Stop)
	}

	// close database
	if config.Get().Database.Driver == "mysql" {
		closes = append(closes, func() error {
			return database.CloseDB()
		})
	}

	// close redis
	if config.Get().App.CacheType == "redis" {
		closes = append(closes, func() error {
			return database.CloseRedis()
		})
	}

	// close tracing
	if config.Get().App.EnableTrace {
		closes = append(closes, func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			return tracer.Close(ctx)
		})
	}

	// close SLS hook (gracefully shutdown SLS producer)
	if slsHookInstance != nil {
		closes = append(closes, func() error {
			return slsHookInstance.Close()
		})
	}

	// close logger
	closes = append(closes, func() error {
		return logger.RouterSync()
	})

	return closes
}
