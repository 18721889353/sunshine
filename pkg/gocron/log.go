package gocron

import (
	"context"

	"github.com/robfig/cron/v3"

	"github.com/18721889353/sunshine/pkg/logger"
)

var (
	SecondType = 0
	MinuteType = 1
)

type options struct {
	isOnlyPrintError bool // default false

	granularity int // 0: second, 1: minute
}

func defaultOptions() *options {
	return &options{
		isOnlyPrintError: false,

		granularity: SecondType,
	}
}

func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// Option set the cron Options.
type Option func(*options)

// WithGranularity set log
func WithGranularity(granularity int) Option {
	return func(o *options) {
		if granularity >= MinuteType {
			granularity = MinuteType
		} else {
			granularity = SecondType
		}
		o.granularity = granularity
	}
}

// WithOnlyPrintError set only print error
func WithOnlyPrintError(enable bool) Option {
	return func(o *options) {
		o.isOnlyPrintError = enable
	}
}

type projectLog struct {
	isOnlyPrintError bool
}

// Info print info
func (l *projectLog) Info(msg string, keysAndValues ...interface{}) {
	if l.isOnlyPrintError {
		return
	}
	if msg == "wake" { // 忽略wake
		return
	}
	msg = "cron_" + msg
	fields := parseKVs(keysAndValues)
	logger.InfoWithCtx(context.Background(), msg, fields...)
}

// Error print error
func (l *projectLog) Error(err error, msg string, keysAndValues ...interface{}) {
	fields := parseKVs(keysAndValues)
	fields = append(fields, logger.Err(err))
	msg = "cron_" + msg
	logger.ErrorWithCtx(context.Background(), msg, fields...)
}

func parseKVs(kvs interface{}) []logger.Field {
	var fields []logger.Field

	infos, ok := kvs.([]interface{})
	if !ok {
		return fields
	}

	l := len(infos)
	if l%2 == 1 {
		return fields
	}

	for i := 0; i < l; i += 2 {
		key := infos[i].(string) //nolint
		value := infos[i+1]

		// replace id with task name
		if key == "entry" {
			if id, ok := value.(cron.EntryID); ok {
				key = "task"
				if v, isExist := idName.Load(id); isExist {
					value = v
				}
			}
		}

		fields = append(fields, logger.Any(key, value))
	}

	return fields
}
