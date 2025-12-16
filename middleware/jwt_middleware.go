package middleware

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var secretKey []byte

func LoadSecret() {
	secretKey = []byte(os.Getenv("JWT_SECRET"))
}

func GetSecret() []byte {
	return secretKey
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		var tokenString string
		var err error

		// 1. INTENTO PRINCIPAL: Buscar en el Header "Authorization" (Estándar para Apps Web)
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			// El formato debe ser "Bearer <token>"
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenString = parts[1]
			}
		}

		// 2. INTENTO SECUNDARIO: Si no hay header, buscar en Cookie (Fallback)
		if tokenString == "" {
			tokenString, err = c.Cookie("token")
		}

		// Si fallaron los dos métodos, abortar
		if tokenString == "" || err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "No autorizado: token ausente"})
			c.Abort()
			return
		}

		// Parsear y validar el token
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			return GetSecret(), nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token inválido o expirado"})
			c.Abort()
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Claims inválidos"})
			c.Abort()
			return
		}

		var userIDStr string
		if v, ok := claims["userId"].(string); ok && v != "" {
			userIDStr = v
		} else if v, ok := claims["userID"].(string); ok && v != "" {
			userIDStr = v
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "userId inválido"})
			c.Abort()
			return
		}

		if _, err := primitive.ObjectIDFromHex(userIDStr); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Formato de userId inválido"})
			c.Abort()
			return
		}

		c.Set("userId", userIDStr)
		c.Next()
	}
}

// Corrección de la Cookie
func SetAuthCookie(c *gin.Context, tokenString string, duration time.Duration) {
	// Leemos si estamos en prod o dev
	appEnv := os.Getenv("APP_ENV")

	maxAge := int(duration.Seconds())

	// IMPORTANTE: Dejar domain vacío.
	// Si pones "verdustock.onrender.com" a veces falla. Dejarlo vacío es más seguro.
	domain := ""

	secure := false
	httpOnly := true // Esto hace que sea invisible para el JS del frontend

	var sameSite http.SameSite

	if appEnv == "production" {
		secure = true                    // Obligatorio para SameSite=None
		sameSite = http.SameSiteNoneMode // Obligatorio para compartir entre dominios distintos
	} else {
		sameSite = http.SameSiteLaxMode
	}

	c.SetSameSite(sameSite)
	// La firma es: name, value, maxAge, path, domain, secure, httpOnly
	c.SetCookie("token", tokenString, maxAge, "/", domain, secure, httpOnly)
}
