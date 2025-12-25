// Package initial is the package that starts the service to initialize the service, including
// the initialization configuration, service configuration, connecting to the database, and
// resource release needed when shutting down the service.
package initial

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"time"

	"github.com/panjf2000/ants/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"

	"github.com/18721889353/sunshine/pkg/jwt"
	v5 "github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap/zapcore"

	"github.com/jinzhu/copier"

	"github.com/18721889353/sunshine/pkg/conf"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/nacoscli"
	"github.com/18721889353/sunshine/pkg/stat"
	"github.com/18721889353/sunshine/pkg/tracer"

	"github.com/18721889353/sunshine/configs"
	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/internal/database"
)

var (
	version            string
	configFile         string
	enableConfigCenter bool
	logPool            *ants.Pool
)

// 初始化协程池（建议在init()中调用）
func initLogPool() {
	var err error
	// 增加池容量到500，并添加非阻塞选项和最大阻塞任务数限制
	logPool, err = ants.NewPool(500,
		ants.WithPreAlloc(true),
		ants.WithNonblocking(false),     // 设置为阻塞模式，确保任务不会丢失
		ants.WithMaxBlockingTasks(1000), // 最多允许1000个任务等待
	)
	if err != nil {
		panic(err)
	}
}

// sendLogToMQ 发送日志到消息队列
func sendLogToMQ(ctx context.Context, entry zapcore.Entry, fields []logger.Field) {
	ctx, span := otel.Tracer(config.Get().App.Name).Start(ctx, "sendLogToMQ")
	defer span.End()
	timeoutCtx, cancelFunc := context.WithTimeout(ctx, time.Second*2)
	defer cancelFunc()

	// 添加重试机制
	var err error
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		err = database.GetRabbitMQ().SendMessage(
			timeoutCtx,
			config.Get().Rabbitmq.DoingOrder.ExchangeName,
			config.Get().Rabbitmq.DoingOrder.NormalQueueName,
			fmt.Sprintf("[%s] [%s] [%s]", entry.Caller.TrimmedPath(), entry.Level, logger.ToJSON(append(fields, zap.String("current_time", entry.Time.Format("2006-01-02 15:04:05.000000")), zap.String("log_msg", entry.Message)))),
			fmt.Sprintf("%v", time.Now().Nanosecond()),
		)
		if err == nil {
			break
		}
		// 如果不是最后一次尝试，等待一段时间后重试
		if i < maxRetries-1 {
			time.Sleep(time.Millisecond * 100 * time.Duration(i+1))
		}
	}

	if err != nil {
		// 记录错误到 span
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		// 同时打印到标准输出，确保关键错误不丢失
		fmt.Printf("Failed to send log to MQ: %v\n", err)
	}
}

func customHook(entry zapcore.Entry, fields []logger.Field) error {
	// 分析字段数据并打印键值对
	// 参数 entry 介绍
	// entry  参数就是单条日志结构体，主要包括字段如下：
	//Level      日志等级
	//Time       当前时间
	//LoggerName  日志名称
	//Message    日志内容
	//Caller     各个文件调用路径
	//Stack      代码调用栈
	// 如果协程池为nil，直接同步发送日志
	if logPool == nil {
		// 同步发送日志，确保不会丢失
		ctx := context.Background()
		sendLogToMQ(ctx, entry, fields)
		return nil
	}
	//这里启动一个协程，hook丝毫不会影响程序性能，
	// 使用协程池
	err := logPool.Submit(func() {
		ctx := context.Background()
		sendLogToMQ(ctx, entry, fields)
	})
	// 如果提交任务失败，采用同步方式发送日志，防止日志丢失
	if err != nil {
		// 记录任务提交失败的错误
		logger.Error("Failed to submit log task to pool",
			zap.Error(err),
			zap.String("log_message", entry.Message),
		)
		// 同步发送日志作为降级处理
		go func() {
			ctx := context.Background()
			sendLogToMQ(ctx, entry, fields)
		}()
	}

	return nil
}

// InitApp initial app configuration
func InitApp() {
	initConfig()
	initLogPool()
	cfg := config.Get()

	// initializing log
	_, err := logger.Init(
		logger.WithLevel(cfg.Logger.Level),
		logger.WithFormat(cfg.Logger.Format),
		//logger.WithCustomHooks(customHook),
		logger.WithAsync(cfg.Logger.IsAsync),
		logger.WithAsyncBufferSize(cfg.Logger.AsyncBufferSize*1048576),
		logger.WithAsyncFlushInterval(time.Duration(cfg.Logger.AsyncFlushInterval)*time.Second),
		logger.WithSave(
			cfg.Logger.IsSave,
			logger.WithSaveDay(cfg.Logger.LogFileConfig.IsSaveDay),
			logger.WithFileName(cfg.Logger.LogFileConfig.Filename),
			logger.WithFileMaxSize(cfg.Logger.LogFileConfig.MaxSize),
			logger.WithFileMaxBackups(cfg.Logger.LogFileConfig.MaxBackups),
			logger.WithFileMaxAge(cfg.Logger.LogFileConfig.MaxAge),
			logger.WithFileIsCompression(cfg.Logger.LogFileConfig.IsCompression),
			logger.WithNoPrint(cfg.Logger.LogFileConfig.IsNoPrint),
		),
	)
	if err != nil {
		panic(err)
	}
	logger.Debug(config.Show())
	logger.Info("[logger] was initialized")

	if cfg.App.OpenJwt {
		var sm *v5.SigningMethodHMAC
		if config.Get().Jwt.SigningMethod == "HS256" {
			sm = jwt.HS256
		} else if config.Get().Jwt.SigningMethod == "HS384" {
			sm = jwt.HS384
		} else {
			sm = jwt.HS512
		}
		jwt.Init(
			jwt.WithExpire(time.Minute*time.Duration(config.Get().Jwt.Expire)),
			jwt.WithSigningKey(config.Get().Jwt.SigningKey),
			jwt.WithSigningMethod(sm),
			jwt.WithIssuer(config.Get().Jwt.Issuer),
		)
		logger.Info("init jwt succeeded")
	}

	// initializing tracing
	if cfg.App.EnableTrace {
		tracer.InitWithConfig(
			cfg.App.Name,
			cfg.App.Env,
			cfg.App.Version,
			cfg.Jaeger.AgentHost,
			strconv.Itoa(cfg.Jaeger.AgentPort),
			cfg.App.TracingSamplingRate,
		)
		logger.Info("[tracer] was initialized")
	}

	// initializing the print system and process resources
	if cfg.App.EnableStat {
		stat.Init(
			stat.WithLog(logger.Get()),
			stat.WithPrintInterval(time.Minute),                                         // 打印统计信息间隔
			stat.WithAlarm(stat.WithCPUThreshold(0.85), stat.WithMemoryThreshold(0.85)), // invalid if it is windows, the default threshold for cpu and memory is 0.8, you can modify them
			stat.WithPrintField(logger.String("service_name", cfg.App.Name), logger.String("host", cfg.App.Host)),
		)
		logger.Info("[resource statistics] was initialized")
	}

	// initializing database
	//if cfg.Database.Driver == "mysql" {
	//	database.InitDB()
	//	logger.Infof("[%s] was initialized", cfg.Database.Driver)
	//}
	//if cfg.App.CacheType == "redis" {
	//	database.InitCache(cfg.App.CacheType)
	//	logger.Infof("[%s] was initialized", cfg.App.CacheType)
	//}
	if int64(cfg.App.MachineID) > 0 {
		database.GetSnowNode()
		logger.Info("init SnowNode  succeeded")
	}
}

func initConfig() {
	flag.StringVar(&version, "version", "", "service Version Number")
	flag.BoolVar(&enableConfigCenter, "enable-cc", false, "whether to get from the configuration center, "+
		"if true, the '-c' parameter indicates the configuration center")
	flag.StringVar(&configFile, "c", "", "configuration file")
	flag.Parse()

	if enableConfigCenter {
		getConfigFromNacos()
	} else {
		getConfigFromLocal()
	}

	if version != "" {
		config.Get().App.Version = version
	}
}

// get the configuration from the configuration center (first get the nacos configuration,
// then read the service configuration according to the nacos configuration center)
func getConfigFromNacos() {
	if configFile == "" {
		configFile = configs.Path("serverNameExample_cc.yml")
	}
	nacosConfig, err := config.NewCenter(configFile)
	if err != nil {
		panic(err)
	}
	appConfig := &config.Config{}
	params := &nacoscli.Params{}
	_ = copier.Copy(params, &nacosConfig.Nacos)
	format, data, err := nacoscli.GetConfig(params)
	if err != nil {
		panic(fmt.Sprintf("connect to configuration center err, %v", err))
	}
	err = conf.ParseConfigData(data, format, appConfig)
	if err != nil {
		panic(fmt.Sprintf("parse configuration data err, %v", err))
	}
	if appConfig.App.Name == "" {
		panic("read the config from center error, config data is empty")
	}
	config.Set(appConfig)
}

// get configuration from local configuration file
func getConfigFromLocal() {
	if configFile == "" {
		configFile = configs.Path("serverNameExample.yml")
	}
	err := config.Init(configFile)
	if err != nil {
		panic("init config error: " + err.Error())
	}
}
