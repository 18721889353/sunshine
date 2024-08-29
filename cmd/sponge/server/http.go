// Package server is a sunshine UI service that contains the front-end pages.
package server

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/18721889353/sunshine/pkg/gin/handlerfunc"
	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/gin/validator"
	"github.com/18721889353/sunshine/pkg/logger"
)

//go:embed static
var staticFS embed.FS // index.html in the static directory

var defaultAddr = "http://localhost:24631"
var frontendDir = "frontend"
var ConfigJsFile = "static/appConfig.js"

// NewRouter create a router
func NewRouter(sunshineAddr string, isLog bool) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Cors())
	if isLog {
		r.Use(middleware.Logging(middleware.WithLog(logger.Get())))
	}
	binding.Validator = validator.Init()

	// solve vue using history route 404 problem
	r.NoRoute(handlerfunc.BrowserRefreshFS(staticFS, "static/index.html"))

	// determine whether you need to use Embed.FS static resources based on the default configured address,
	// if it is not the default address, copy the read-only Embed.FS static resources locally and then modify the default
	// configured address, so dynamically configure the service address based on the parameter.
	if checkIsUseEmbedFS(frontendDir, sunshineAddr) {
		r.GET("/static/*filepath", func(c *gin.Context) {
			staticServer := http.FileServer(http.FS(staticFS))
			staticServer.ServeHTTP(c.Writer, c.Request)
		})
	} else {
		r.GET("/static/*filepath", func(c *gin.Context) {
			localFileDir := filepath.Join(frontendDir, "static")
			filePath := c.Param("filepath")
			c.File(localFileDir + filePath)
		})
	}

	apiV1 := r.Group("/api/v1")
	apiV1.POST("/generate", GenerateCode)
	apiV1.POST("/uploadFiles", UploadFiles)
	apiV1.POST("/listTables", ListTables)
	apiV1.GET("/listDrivers", ListDbDrivers)
	apiV1.GET("/record/:path", GetRecord)

	return r
}

// RunHTTPServer run http server
func RunHTTPServer(sunshineAddr string, port int, isLog bool) {
	initRecord()

	router := NewRouter(sunshineAddr, isLog)
	server := &http.Server{
		Addr:           fmt.Sprintf(":%d", port),
		Handler:        router,
		MaxHeaderBytes: 1 << 20,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(fmt.Errorf("ListenAndServe error: %v", err))
	}
}

func checkIsUseEmbedFS(targetDir string, sunshineAddr string) bool {
	if sunshineAddr == defaultAddr {
		return true
	}
	err := saveFSToLocal(targetDir, sunshineAddr)
	if err != nil {
		panic(err)
	}
	return false
}

func saveFSToLocal(targetDir string, sunshineAddr string) error {
	_ = os.RemoveAll(filepath.Join(targetDir, "static"))
	time.Sleep(time.Millisecond * 10)

	// Walk through the embedded filesystem
	return fs.WalkDir(staticFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Create the corresponding directory structure locally
		localPath := filepath.Join(targetDir, path)
		if d.IsDir() {
			err := os.MkdirAll(localPath, 0755)
			if err != nil {
				return err
			}
		} else {
			// Read the file from the embedded filesystem
			content, err := fs.ReadFile(staticFS, path)
			if err != nil {
				return err
			}

			// replace config address
			if path == ConfigJsFile {
				content = bytes.ReplaceAll(content, []byte(defaultAddr), []byte(sunshineAddr))
			}

			// Save the content to the local file
			err = os.WriteFile(localPath, content, 0644)
			if err != nil {
				return err
			}
		}

		return nil
	})
}
