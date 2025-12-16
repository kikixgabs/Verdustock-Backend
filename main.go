package main

import (
	"fmt"
	"log"
	"os"

	"verdustock-auth/database"
	"verdustock-auth/handlers"
	"verdustock-auth/middleware"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {

	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}

	if env == "development" {
		if err := godotenv.Load(".env.development"); err != nil {
			log.Println("⚠️ No se pudo cargar .env.development, usando variables del sistema")
		}
	} else {
		if err := godotenv.Load(".env.production"); err != nil {
			log.Println("⚠️ No se pudo cargar .env.production, usando variables del sistema")
		}
	}

	middleware.LoadSecret()

	mongoURI := os.Getenv("MONGODB_URI")
	dbName := os.Getenv("MONGODB_NAME")

	database.Connect(mongoURI, dbName)

	// Initialize Catalog if empty
	if err := handlers.InitializeCatalog(); err != nil {
		log.Println("⚠️ Advertencia: No se pudo inicializar el catálogo de productos:", err)
	}

	router := gin.Default()

	allowedOrigins := []string{
		"http://localhost:4200",
		"https://kikixgabs.github.io",
	}

	router.Use(func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		for _, o := range allowedOrigins {
			if o == origin {
				c.Writer.Header().Set("Access-Control-Allow-Origin", o)
				break
			}
		}
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	router.POST("/login", handlers.LoginHandler)
	router.POST("/logout", handlers.LogoutHandler)

	router.GET("/auth/me", handlers.AuthMeHandler(database.UserCollection))
	router.POST("/admin/create-user", handlers.AdminCreateUserHandler)

	// Webhooks
	router.POST("/webhooks/mercadopago", handlers.HandleMPWebhook)

	userGroup := router.Group("/user")
	userGroup.Use(middleware.AuthMiddleware())
	{
		userGroup.POST("/mercadopago/link", handlers.LinkMPAccountHandler)
	}

	stockGroup := router.Group("/stock")
	stockGroup.Use(middleware.AuthMiddleware())
	{
		stockGroup.GET("", handlers.GetStockHandler)
		stockGroup.PUT("/:id", handlers.UpdateProductHandler)
		stockGroup.POST("", handlers.CreateProductHandler)

	}

	sellsGroup := router.Group("/sells")
	sellsGroup.Use(middleware.AuthMiddleware())
	{
		sellsGroup.POST("", handlers.CreateSellHandler)
		sellsGroup.GET("", handlers.GetSellsHandler)
		sellsGroup.PUT("/:id", handlers.UpdateSellHandler)
		sellsGroup.POST("/close", handlers.CloseBoxHandler)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Println("INFO: PORT not set, defaulting to " + port)
	}

	fmt.Printf("🚀 Servidor corriendo en modo %s en http://localhost:%s\n", env, port)
	router.Run(":" + port)
}
