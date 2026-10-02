package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

// The native task plugin owns submission/query; these host routes provide
// authenticated browser discovery, small reference uploads and owned results.
func SetQisiLikeAIRouter(router *gin.Engine) {
	api := router.Group("/likeai", middleware.RouteTag("relay"), controller.QisiLikeAIBearerOnly, middleware.TokenAuth())
	api.GET("/task/models", controller.QisiLikeAIListModels)
	api.POST("/files", middleware.UserCriticalRateLimit("qisi-likeai-upload"), middleware.UploadRateLimit(), controller.QisiLikeAIUpload)
	api.GET("/task/artifact/:task_id/:kind/:index", controller.QisiLikeAIArtifact)
	api.HEAD("/task/artifact/:task_id/:kind/:index", controller.QisiLikeAIArtifact)
}
