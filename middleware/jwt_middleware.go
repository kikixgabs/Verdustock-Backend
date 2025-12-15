package middleware

import (
	"net/http"
	"os"
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
		tokenString, err := c.Cookie("token")
		if err != nil || tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "No autorizado: token ausente"})
			c.Abort()
			return
		}

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

func SetAuthCookie(c *gin.Context, tokenString string, duration time.Duration) {
	env := os.Getenv("APP_ENV")

	maxAge := int(duration.Seconds())
	domain := ""
	secure := false

	if env == "production" {
		domain = "auth-backend-production-414c.up.railway.app"
		secure = true
	}

	c.SetCookie("token", tokenString, maxAge, "/", domain, secure, true)

	if env == "production" {
		c.SetSameSite(http.SameSiteNoneMode)
	} else {
		c.SetSameSite(http.SameSiteLaxMode)
	}
}
