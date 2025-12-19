package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"verdustock-auth/database"
	"verdustock-auth/handlers"
	"verdustock-auth/middleware"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {

	// 1. Carga de variables de entorno
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}

	if env == "development" {
		if err := godotenv.Load(".env.development"); err != nil {
			log.Println("⚠️ Aviso: No hay .env.development, usando variables de sistema")
		}
	} else {
		if err := godotenv.Load(".env.production"); err != nil {
			log.Println("ℹ️ Info: Corriendo con variables de entorno del sistema (Render)")
		}
	}

	// 2. Inicialización de Secretos y Base de Datos
	middleware.LoadSecret()

	mongoURI := os.Getenv("MONGODB_URI")
	dbName := os.Getenv("MONGODB_NAME")

	if mongoURI == "" {
		log.Fatal("❌ Error Fatal: MONGODB_URI no está definida")
	}

	database.Connect(mongoURI, dbName)

	if err := handlers.InitializeCatalog(); err != nil {
		log.Println("⚠️ Advertencia: No se pudo inicializar el catálogo de productos:", err)
	}

	// 3. Configuración del Servidor y CORS
	router := gin.Default()

	config := cors.DefaultConfig()
	config.AllowOrigins = []string{
		"http://localhost:4200",       // Local
		"https://kikixgabs.github.io", // Producción
	}
	config.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{
		"Origin",
		"Content-Type",
		"Accept",
		"Authorization",
		"X-Requested-With",
		"X-Admin-Secret",
	}
	config.AllowCredentials = true
	config.MaxAge = 12 * time.Hour

	router.Use(cors.New(config))

	// 4. Definición de Rutas
	router.POST("/login", handlers.LoginHandler)
	router.POST("/logout", handlers.LogoutHandler)

	router.GET("/auth/me", handlers.AuthMeHandler(database.UserCollection))
	router.POST("/admin/create-user", handlers.AdminCreateUserHandler)

	// Webhooks
	router.POST("/webhooks/mercadopago", handlers.HandleMPWebhook)

	// Rutas de Pagos y Caja (Protegidas)
	router.GET("/payments", middleware.AuthMiddleware(), handlers.GetMPPaymentsHandler)
	router.POST("/payments/sync", middleware.AuthMiddleware(), handlers.SyncMPTransfersHandler)

	// ✅ NUEVA RUTA: Verificar cajas pendientes (Solución al error 404)
	router.GET("/cash/pending", middleware.AuthMiddleware(), handlers.CheckPendingBoxesHandler)

	// Grupo User (Protegido)
	userGroup := router.Group("/user")
	userGroup.Use(middleware.AuthMiddleware())
	{
		userGroup.POST("/mercadopago/link", handlers.LinkMPAccountHandler)
	}

	// Grupo Stock (Protegido)
	stockGroup := router.Group("/stock")
	stockGroup.Use(middleware.AuthMiddleware())
	{
		stockGroup.GET("", handlers.GetStockHandler)
		stockGroup.PUT("/:id", handlers.UpdateProductHandler)
		stockGroup.POST("", handlers.CreateProductHandler)
	}

	// Grupo Ventas (Protegido)
	sellsGroup := router.Group("/sells")
	sellsGroup.Use(middleware.AuthMiddleware())
	{
		sellsGroup.POST("", handlers.CreateSellHandler)
		sellsGroup.GET("", handlers.GetSellsHandler)
		sellsGroup.PUT("/:id", handlers.UpdateSellHandler)
		sellsGroup.POST("/close", handlers.CloseBoxHandler)
	}

	// 5. Iniciar Servidor
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Println("INFO: PORT not set, defaulting to " + port)
	}

	fmt.Printf("🚀 Servidor corriendo en modo %s en puerto :%s\n", env, port)
	router.Run(":" + port)
}
