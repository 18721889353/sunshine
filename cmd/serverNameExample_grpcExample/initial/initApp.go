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

	"github.com/jinzhu/copier"

	"github.com/18721889353/sunshine/configs"
	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/pkg/conf"
	"github.com/18721889353/sunshine/pkg/jwt"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/nacoscli"
	"github.com/18721889353/sunshine/pkg/stat"
	"github.com/18721889353/sunshine/pkg/tracer"

	v5 "github.com/golang-jwt/jwt/v5"
)

var (
	version            string
	configFile         string
	enableConfigCenter bool
	initCtx            = context.Background() // 初始化阶段使用的 context
	slsHookInstance    *logger.SLSHook        // SLS Hook 实例，用于优雅关闭
)

// InitApp initial app configuration
func InitApp() {
	initConfig()
	cfg := config.Get()

	// 初始化 SLS Hook（如果启用）- 注意：此时 Logger 尚未初始化，不要记录日志
	if cfg.Sls.Enable {
		// 如果未配置 Source，默认使用应用名称作为日志来源
		source := cfg.Sls.Source
		if source == "" {
			source = cfg.App.Name
		}
		slsConfig := &logger.SLSConfig{
			Endpoint:              cfg.Sls.Endpoint,
			AccessKeyID:           cfg.Sls.AccessKeyID,
			AccessKeySecret:       cfg.Sls.AccessKeySecret,
			ProjectName:           cfg.Sls.Project,
			LogStoreName:          cfg.Sls.Logstore,
			Topic:                 cfg.Sls.Topic,
			Source:                source,
			MaxRetries:            cfg.Sls.Retries,
			Timeout:               cfg.Sls.Timeout,                 // 从配置文件读取超时时间
			TotalSizeLnBytes:      int64(cfg.Sls.TotalSizeLnBytes), // 缓存总大小(字节)
			MaxBatchCount:         cfg.Sls.MaxBatchCount,           // 单个 Batch 最大日志条数
			MaxBatchSize:          cfg.Sls.MaxBatchSize,            // 单个 Batch 最大大小(字节)
			LingerMs:              cfg.Sls.LingerMs,                // Batch 刷新间隔(毫秒)
			DisableRuntimeMetrics: cfg.Sls.DisableRuntimeMetrics,   // 禁用运行时指标日志
			EnableHealthCheck:     cfg.Sls.EnableHealthCheck,       // 从配置文件读取是否启用健康检查
			HealthCheckInterval:   cfg.Sls.HealthCheckInterval,     // 健康检查间隔(秒)
			SendTimeout:           cfg.Sls.SendTimeout,             // 发送超时时间(秒)
		}

		var err error
		slsHookInstance, err = logger.NewSLSHook(slsConfig)
		if err != nil {
			// SLS 启动失败，直接终止服务（此时不能用 logger，用 fmt）
			panic(fmt.Sprintf("failed to init SLS hook: %v", err))
		}
		// 注意：这里不能记录日志，因为 Logger 还没初始化
		// logger.InfoWithCtx(initCtx, "[SLS hook] was initialized", ...)
	}

	// initializing log
	_, err := logger.Init(
		logger.WithLevel(cfg.Logger.Level),
		logger.WithFormat(cfg.Logger.Format),
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
		// 注册 SLS Hook（如果启用）
		func() logger.Option {
			if slsHookInstance != nil {
				return logger.WithCustomHooksWithCtx(slsHookInstance.Hook)
			}
			// 返回 nil 表示不添加任何 Option
			return nil
		}(),
		// 日志路由配置 - 支持按模块/级别动态路由到不同文件
		logger.WithRoutes(func() []*logger.RouteConfig {
			if len(cfg.Logger.Routes) == 0 {
				return nil
			}
			routes := make([]*logger.RouteConfig, 0, len(cfg.Logger.Routes))
			for _, r := range cfg.Logger.Routes {
				routes = append(routes, &logger.RouteConfig{
					Module:    r.Module,
					Filename:  r.Filename, // 支持完整路径或相对路径
					MaxSize:   r.MaxSize,
					MaxAge:    r.MaxAge,
					IsSaveDay: r.IsSaveDay,
					Format:    r.Format,
					IsAsync:   r.IsAsync,
				})
			}
			return routes
		}()),
	)
	if err != nil {
		panic(err)
	}

	logger.InfoWithCtx(initCtx, "[logger] was initialized")

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
		logger.InfoWithCtx(initCtx, "init jwt succeeded")
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
			cfg.Jaeger.Endpoint, // 添加 endpoint 参数
		)
		logger.InfoWithCtx(initCtx, "[tracer] was initialized")
	}

	// initializing the print system and process resources
	if cfg.App.EnableStat {
		stat.Init(
			stat.WithPrintInterval(time.Minute),                                         // 打印统计信息间隔
			stat.WithAlarm(stat.WithCPUThreshold(0.85), stat.WithMemoryThreshold(0.85)), // invalid if it is windows, the default threshold for cpu and memory is 0.8, you can modify them
		)
		logger.InfoWithCtx(initCtx, "[resource statistics] was initialized")
	}

	// initializing database
	if cfg.Database.Driver == "mysql" {
		database.InitDB()
		logger.InfoWithCtx(initCtx, fmt.Sprintf("[%s] was initialized", cfg.Database.Driver))
	}
	if cfg.App.CacheType == "redis" {
		database.InitCache(cfg.App.CacheType)
		logger.InfoWithCtx(initCtx, fmt.Sprintf("[%s] was initialized", cfg.App.CacheType))
	}
	if cfg.Elasticsearch.IsOpen {
		database.InitElasticsearch()
		logger.InfoWithCtx(initCtx, "[Elasticsearch] was initialized")
	}
	if int64(cfg.App.MachineID) > 0 {
		database.GetSnowNode()
		logger.InfoWithCtx(initCtx, "init SnowNode  succeeded")
	}
	if cfg.Rabbitmq.Enable {
		database.InitRabbitmq()
		logger.InfoWithCtx(initCtx, "init RabbitMQ succeeded")
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
	if copyErr := copier.Copy(params, &nacosConfig.Nacos); copyErr != nil {
		panic(fmt.Sprintf("copy nacos config error: %v", copyErr))
	}
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
