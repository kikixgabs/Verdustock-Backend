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
	"go.mongodb.org/mongo-driver/mongo"
)

// Estructura para devolver los días pendientes
type PendingBox struct {
	Date        time.Time     `json:"date"`
	TotalAmount float64       `json:"totalAmount"`
	Count       int           `json:"count"`
	Sells       []models.Sell `json:"sells"` // Opcional: si quieres mandar las ventas de una vez
}

func CheckPendingBoxesHandler(c *gin.Context) {
	userIDStr, _ := c.Get("userId")
	userID, _ := primitive.ObjectIDFromHex(userIDStr.(string))

	// 1. Calcular el inicio del día de HOY (00:00:00)
	loc := time.FixedZone("ART", -3*60*60) // Ajusta a tu zona horaria
	now := time.Now().In(loc)
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 2. Pipeline de Agregación
	// Buscamos: Ventas del usuario + No Cerradas + Fecha < Hoy
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{
			{Key: "userId", Value: userID},
			{Key: "isClosed", Value: false},
			{Key: "date", Value: bson.D{{Key: "$lt", Value: startOfToday}}}, // Menor a hoy
		}}},
		// Agrupamos por día (año-mes-día) para detectar cajas separadas
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{
				{Key: "year", Value: bson.D{{Key: "$year", Value: "$date"}}},
				{Key: "month", Value: bson.D{{Key: "$month", Value: "$date"}}},
				{Key: "day", Value: bson.D{{Key: "$dayOfMonth", Value: "$date"}}},
			}},
			{Key: "totalAmount", Value: bson.D{{Key: "$sum", Value: "$amount"}}},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "date", Value: bson.D{{Key: "$first", Value: "$date"}}},  // Tomamos una fecha de referencia
			{Key: "sells", Value: bson.D{{Key: "$push", Value: "$$ROOT"}}}, // Guardamos las ventas
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "date", Value: 1}}}}, // Las más viejas primero
	}

	cursor, err := database.SellsCollection.Aggregate(ctx, pipeline)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error buscando cajas pendientes"})
		return
	}
	defer cursor.Close(ctx)

	var results []PendingBox
	// Mapeamos el resultado de mongo a nuestra estructura (simplificando el _id complejo)
	for cursor.Next(ctx) {
		var item struct {
			Date        time.Time     `bson:"date"`
			TotalAmount float64       `bson:"totalAmount"`
			Count       int           `bson:"count"`
			Sells       []models.Sell `bson:"sells"`
		}
		if err := cursor.Decode(&item); err == nil {
			results = append(results, PendingBox{
				Date:        item.Date,
				TotalAmount: item.TotalAmount,
				Count:       item.Count,
				Sells:       item.Sells,
			})
		}
	}

	c.JSON(http.StatusOK, results)
}
