// @title ROVO-2API
// @version 1.0.0
// @description ROVO-2API
// @BasePath
package main

import (
	"fmt"
	"os"
	"rovo2api/check"
	"rovo2api/common"
	"rovo2api/common/config"
	logger "rovo2api/common/loggger"
	"rovo2api/middleware"
	"rovo2api/model"
	"rovo2api/router"

	"github.com/gin-gonic/gin"
)

//var buildFS embed.FS

func main() {
	logger.SetupLogger()
	logger.SysLog(fmt.Sprintf("rovo2api %s starting...", common.Version))

	check.CheckEnvVariable()

	if os.Getenv("GIN_MODE") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	var err error

	model.InitTokenEncoders()
	config.InitSGCookies()

	server := gin.New()
	server.Use(gin.Recovery())
	server.Use(middleware.RequestId())
	middleware.SetUpLogger(server)

	// 设置API路由
	router.SetApiRouter(server)
	// 设置前端路由
	//router.SetWebRouter(server, buildFS)

	var port = os.Getenv("PORT")
	if port == "" {
		port = "10111"
	}

	if config.DebugEnabled {
		logger.SysLog("running in DEBUG mode.")
	}

	logger.SysLog("rovo2api start success. enjoy it! ^_^\n")

	err = server.Run(":" + port)

	if err != nil {
		logger.FatalLog("failed to start HTTP server: " + err.Error())
	}
}
