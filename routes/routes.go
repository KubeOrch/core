package routes

import (
	"github.com/KubeOrchestra/core/handlers"
	"github.com/gin-gonic/gin"
)

func SetupRouter() *gin.Engine {
	r := gin.Default()

	v1 := r.Group("/v1")
	{
		v1.GET("/", handlers.HelloHandler)
	}

	return r
}
