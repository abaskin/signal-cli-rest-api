package main

import (
	"flag"

	"github.com/abaskin/signald-rest-api/api"
	_ "github.com/abaskin/signald-rest-api/docs"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title Signal Cli REST API
// @version 1.0
// @description This is the Signal Cli REST API documentation.

// @tag.name General
// @tag.description List general information.

// @tag.name Devices
// @tag.description Register and link Devices.

// @tag.name Groups
// @tag.description Create, List and Delete Signal Groups.

// @tag.name Messages
// @tag.description Send and Receive Signal Messages.

// @host 127.0.0.1:8080
// @BasePath /
func main() {
	signaldSocketPath := flag.String("signald-socket-path", "/var/run/signald/signald.sock", "signald socket path")
	attachmentTmpDir := flag.String("attachment-tmp-dir", "/tmp/", "Attachment tmp directory")
	flag.Parse()

	router := gin.Default()
	// gin.SetMode(gin.ReleaseMode)

	log.Info("Started signald REST API")

	api := api.NewApi(*signaldSocketPath, *attachmentTmpDir)
	v1 := router.Group("/v1")
	{
		about := v1.Group("/about")
		{
			about.GET("", api.About)
		}

		register := v1.Group("/register")
		{
			register.POST(":number", api.RegisterNumber)
			register.POST(":number/verify/:token", api.VerifyRegisteredNumber)
		}

		sendV1 := v1.Group("/send")
		{
			sendV1.POST("", api.Send)
		}

		receive := v1.Group("/receive")
		{
			receive.GET(":number", api.Receive)
		}

		groups := v1.Group("/groups")
		{
			groups.POST(":number", api.CreateGroup)
			groups.GET(":number", api.GetGroups)
			groups.DELETE(":number/:groupid", api.DeleteGroup)
		}

		link := v1.Group("link")
		{
			link.GET("", api.Link)
		}
	}

	v2 := router.Group("/v2")
	{
		sendV2 := v2.Group("/send")
		{
			sendV2.POST("", api.SendV2)
		}
	}

	swaggerUrl := ginSwagger.URL("http://127.0.0.1:8080/swagger/doc.json")
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, swaggerUrl))

	router.Run()
}
