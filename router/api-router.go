package router

import (
	"fmt"
	_ "rovo2api/docs"

	"github.com/gin-gonic/gin"

	"rovo2api/common/config"
	"rovo2api/controller"
	"rovo2api/middleware"
	"strings"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func SetApiRouter(router *gin.Engine) {
	router.Use(middleware.CORS())
	router.Use(middleware.IPBlacklistMiddleware())
	router.Use(middleware.RequestRateLimit())

	if config.SwaggerEnable == "" || config.SwaggerEnable == "1" {
		router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	// *有静态资源时注释此行
	router.GET("/")

	v1Router := router.Group(fmt.Sprintf("%s/v1", ProcessPath(config.RoutePrefix)))

	if !config.CustomHeaderKeyEnabled == true {
		v1Router.Use(middleware.OpenAIAuth())
	}
	v1Router.POST("/chat/completions", controller.ChatForOpenAI)
	//v1Router.POST("/images/generations", controller.ImagesForOpenAI)
	v1Router.GET("/models", controller.OpenaiModels)

}

func ProcessPath(path string) string {
	// 判断字符串是否为空
	if path == "" {
		return ""
	}

	// 判断开头是否为/，不是则添加
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// 判断结尾是否为/，是则去掉
	if strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
	}

	return path
}
