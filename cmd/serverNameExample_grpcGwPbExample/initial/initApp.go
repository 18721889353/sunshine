// Package initial is the package that starts the service to initialize the service, including
// the initialization configuration, service configuration, connecting to the database, and
// resource release needed when shutting down the service.
package initial

import (
	"flag"
	"fmt"
	"github.com/18721889353/sunshine/internal/model"
	"github.com/18721889353/sunshine/pkg/jwt"
	v5 "github.com/golang-jwt/jwt/v5"
	"github.com/jinzhu/copier"
	"go.uber.org/zap/zapcore"
	"strconv"
	"time"

	"github.com/18721889353/sunshine/pkg/conf"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/nacoscli"
	"github.com/18721889353/sunshine/pkg/stat"
	"github.com/18721889353/sunshine/pkg/tracer"

	"github.com/18721889353/sunshine/configs"
	"github.com/18721889353/sunshine/internal/config"
	//"github.com/18721889353/sunshine/internal/rpcclient"
)

var (
	version            string
	configFile         string
	enableConfigCenter bool
)

func ZapLogHandler(entry zapcore.Entry) error {

	// 参数 entry 介绍
	// entry  参数就是单条日志结构体，主要包括字段如下：
	//Level      日志等级
	//Time       当前时间
	//LoggerName  日志名称
	//Message    日志内容
	//Caller     各个文件调用路径
	//Stack      代码调用栈
	//这里启动一个协程，hook丝毫不会影响程序性能，
	go func(paramEntry zapcore.Entry) {
		//logServiceV1.NewAdminLogServiceClient(rpcclient.GetAdminLogServiceRPCConn()).Add(context.Background(), &logServiceV1.AdminLogAddRequest{
		//	Body:     entry.Message,
		//	LogLevel: int32(entry.Level),
		//})
	}(entry)

	return nil
}

// InitApp initial app configuration
func InitApp() {
	initConfig()
	cfg := config.Get()

	// initializing log
	_, err := logger.Init(
		logger.WithLevel(cfg.Logger.Level),
		logger.WithFormat(cfg.Logger.Format),
		logger.WithHooks(ZapLogHandler),
		logger.WithSave(
			cfg.Logger.IsSave,
			logger.WithFileName(cfg.Logger.LogFileConfig.Filename),
			logger.WithFileMaxSize(cfg.Logger.LogFileConfig.MaxSize),
			logger.WithFileMaxBackups(cfg.Logger.LogFileConfig.MaxBackups),
			logger.WithFileMaxAge(cfg.Logger.LogFileConfig.MaxAge),
			logger.WithFileIsCompression(cfg.Logger.LogFileConfig.IsCompression),
		),
	)
	if err != nil {
		panic(err)
	}
	logger.Debug(config.Show())
	logger.Info("init logger succeeded")

	//model.GetDB()
	//logger.Infof("[%s] was initialized", cfg.Database.Driver)
	//
	//model.InitCache(cfg.App.CacheType)
	//logger.Info("init " + cfg.App.CacheType + " succeeded")

	model.GetSnowNode()
	logger.Info("init SnowNode  succeeded")

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
			stat.WithAlarm(), // invalid if it is windows, the default threshold for cpu and memory is 0.8, you can modify themstat.WithPrintField(logger.String("service_name", cfg.App.Name), logger.String("host", cfg.App.Host)),
			stat.WithPrintField(logger.String("service_name", cfg.App.Name), logger.String("host", cfg.App.Host)),
		)
		logger.Info("[resource statistics] was initialized")
	}

	// initializing the rpc server connection
	// example:
	//rpcclient.NewServerNameExampleRPCConn()
}

func initConfig() {
	flag.StringVar(&version, "version", "", "service Version Number")
	flag.BoolVar(&enableConfigCenter, "enable-cc", false, "whether to get from the configuration center, "+
		"if true, the '-c' parameter indicates the configuration center")
	flag.StringVar(&configFile, "c", "", "configuration file")
	flag.Parse()

	if enableConfigCenter {
		// get the configuration from the configuration center (first get the nacos configuration,
		// then read the service configuration according to the nacos configuration center)
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
	} else {
		// get configuration from local configuration file
		if configFile == "" {
			configFile = configs.Path("serverNameExample.yml")
		}
		err := config.Init(configFile)
		if err != nil {
			panic("init config error: " + err.Error())
		}
	}

	if version != "" {
		config.Get().App.Version = version
	}
}
