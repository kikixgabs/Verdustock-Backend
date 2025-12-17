package handlers

import (
	"context"
	"net/http"
	"os"
	"time"
	"verdustock-auth/database"
	"verdustock-auth/models"

	"verdustock-auth/middleware"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
)

func LoginHandler(c *gin.Context) {
	var creds struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		RememberMe bool   `json:"rememberMe"`
	}
	if err := c.ShouldBindJSON(&creds); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Datos inválidos"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var expiration time.Duration
	if creds.RememberMe {
		expiration = 30 * 24 * time.Hour
	} else {
		expiration = 24 * time.Hour
	}

	var user models.User
	err := database.UserCollection.FindOne(ctx, bson.M{"email": creds.Email}).Decode(&user)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Credenciales inválidas"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(creds.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Credenciales inválidas"})
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"userId": user.ID.Hex(),
		"exp":    time.Now().Add(expiration).Unix(),
	})

	tokenString, _ := token.SignedString(middleware.GetSecret())

	middleware.SetAuthCookie(c, tokenString, expiration)
	c.JSON(http.StatusOK, gin.H{"message": "Logueado correctamente"})
}

func LogoutHandler(c *gin.Context) {
	c.SetCookie(
		"token",
		"",
		-1,
		"/",
		"",
		false,
		true,
	)
	c.JSON(http.StatusOK, gin.H{"message": "Sesión cerrada correctamente"})
}

func AuthMeHandler(userCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {

		tokenString, err := c.Cookie("token")
		if err != nil || tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario no autenticado"})
			return
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			return middleware.GetSecret(), nil
		})
		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token inválido o expirado"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Claims inválidos"})
			return
		}

		var userIDStr string
		if v, ok := claims["userId"].(string); ok && v != "" {
			userIDStr = v
		} else if v, ok := claims["userID"].(string); ok && v != "" {
			userIDStr = v
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "userId inválido"})
			return
		}

		userID, err := primitive.ObjectIDFromHex(userIDStr)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Formato de userId inválido"})
			return
		}

		var user models.User
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = userCollection.FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario no encontrado"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"user": gin.H{
				"id":                 user.ID.Hex(),
				"email":              user.Email,
				"username":           user.Username,
				"theme":              user.Theme,
				"language":           user.Language,
				"mpAccountConnected": user.MPAccountConnected,
			},
		})
	}
}

func AdminCreateUserHandler(c *gin.Context) {
	adminSecret := c.GetHeader("X-Admin-Secret")
	expectedSecret := os.Getenv("ADMIN_SECRET_KEY")

	if expectedSecret == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server misconfiguration: ADMIN_SECRET_KEY not set"})
		return
	}

	if adminSecret != expectedSecret {
		c.JSON(http.StatusForbidden, gin.H{"error": "Acceso denegado"})
		return
	}

	// ✅ SOLUCIÓN: Usamos una estructura auxiliar para recibir los datos
	// Esto permite leer el "password" del JSON aunque el modelo User lo tenga oculto.
	var input struct {
		Username string `json:"username" binding:"required"`
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	// BindJSON ahora usa 'input' en vez de 'user'
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Faltan datos: Email, usuario y contraseña son requeridos"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check if user already exists
	var existing models.User
	err := database.UserCollection.FindOne(ctx, bson.M{"email": input.Email}).Decode(&existing)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "El email ya está registrado"})
		return
	}

	// Encrypt password (Usamos input.Password)
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al procesar contraseña"})
		return
	}

	// Creamos el modelo User real manualmente
	newUser := models.User{
		ID:       primitive.NewObjectID(),
		Email:    input.Email,
		Username: input.Username,
		Password: string(hash), // Guardamos el hash
		Theme:    "light",      // Valores por defecto
		Language: "es",
		// MPAccount queda vacío/nil por defecto
	}

	// Save to database
	res, err := database.UserCollection.InsertOne(ctx, newUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al registrar usuario"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Usuario creado exitosamente",
		"userId":  res.InsertedID,
	})
}
