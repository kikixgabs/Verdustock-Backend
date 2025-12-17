package handlers

import (
	"context"
	"net/http"
	"time"

	"verdustock-auth/database"
	"verdustock-auth/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// GetMPPaymentsHandler obtiene el historial de pagos de Mercado Pago
func GetMPPaymentsHandler(c *gin.Context) {
	// 1. Obtener ID del usuario logueado
	userIDStr, exists := c.Get("userId")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	userID, _ := primitive.ObjectIDFromHex(userIDStr.(string))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 2. Buscar en la colección MPPayments
	// Filtramos por userId y ordenamos por fecha descendente (lo más nuevo arriba)
	opts := options.Find().SetSort(bson.D{{Key: "receivedAt", Value: -1}})

	cursor, err := database.MPPaymentsCollection.Find(ctx, bson.M{"userId": userID}, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al obtener pagos"})
		return
	}
	defer cursor.Close(ctx)

	// 3. Decodificar resultados
	var payments []models.MPPayment
	if err = cursor.All(ctx, &payments); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error procesando datos"})
		return
	}

	// Si no hay pagos, devolver array vacío en vez de null
	if payments == nil {
		payments = []models.MPPayment{}
	}

	c.JSON(http.StatusOK, payments)
}
