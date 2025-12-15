package handlers

import (
	"context"
	"log"
	"net/http"
	"time"
	"verdustock-auth/database"
	"verdustock-auth/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// CreateSellHandler creates a new sell record
func CreateSellHandler(c *gin.Context) {
	userIDStr, exists := c.Get("userId")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario no identificado"})
		return
	}
	userID, _ := primitive.ObjectIDFromHex(userIDStr.(string))

	var input struct {
		Amount   float64         `json:"amount"`
		Type     models.SellType `json:"type"`
		Comments string          `json:"comments"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Datos inválidos"})
		return
	}

	sell := models.Sell{
		ID:       primitive.NewObjectID(),
		UserID:   userID,
		Amount:   input.Amount,
		Date:     time.Now(),
		Type:     input.Type,
		Comments: input.Comments,
		Modified: false,
		IsClosed: false,
		History:  []models.SellHistory{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := database.SellsCollection.InsertOne(ctx, sell)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al registrar venta"})
		return
	}

	c.JSON(http.StatusCreated, sell)
}

// GetSellsHandler retrieves sells based on filters (open/closed)
func GetSellsHandler(c *gin.Context) {
	userIDStr, exists := c.Get("userId")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario no identificado"})
		return
	}
	userID, _ := primitive.ObjectIDFromHex(userIDStr.(string))

	// Query params: status (open/closed), date (optional)
	status := c.Query("status") // "open" or "closed"
	dateParam := c.Query("date")

	filter := bson.M{"userId": userID}

	if status == "open" {
		filter["isClosed"] = false
	} else if status == "closed" {
		filter["isClosed"] = true
	}

	if dateParam != "" {
		// Expecting YYYY-MM-DD
		parsedDate, err := time.Parse("2006-01-02", dateParam)
		if err == nil {
			// Find sells within that day
			nextDay := parsedDate.Add(24 * time.Hour)
			filter["date"] = bson.M{
				"$gte": parsedDate,
				"$lt":  nextDay,
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cursor, err := database.SellsCollection.Find(ctx, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al obtener ventas"})
		return
	}
	defer cursor.Close(ctx)

	var sells []models.Sell
	if err := cursor.All(ctx, &sells); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al procesar ventas"})
		return
	}
	if sells == nil {
		sells = []models.Sell{}
	}

	c.JSON(http.StatusOK, sells)
}

// UpdateSellHandler updates a sell if it is open
func UpdateSellHandler(c *gin.Context) {
	idStr := c.Param("id")
	objID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}

	userIDStr, exists := c.Get("userId")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario no identificado"})
		return
	}
	userID, _ := primitive.ObjectIDFromHex(userIDStr.(string))

	var input struct {
		Amount   *float64         `json:"amount"`
		Type     *models.SellType `json:"type"`
		Comments *string          `json:"comments"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		log.Printf("Error binding JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Datos inválidos: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Fetch existing sell
	var existingSell models.Sell
	err = database.SellsCollection.FindOne(ctx, bson.M{"_id": objID, "userId": userID}).Decode(&existingSell)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Venta no encontrada"})
		return
	}

	if existingSell.IsClosed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No se puede modificar una venta de una caja cerrada"})
		return
	}

	// 2. Track changes
	var newHistory []models.SellHistory
	isModified := false
	now := time.Now()

	updateFields := bson.M{}

	if input.Amount != nil && *input.Amount != existingSell.Amount {
		newHistory = append(newHistory, models.SellHistory{
			Date:     now,
			Field:    "amount",
			OldValue: existingSell.Amount,
			NewValue: *input.Amount,
		})
		updateFields["amount"] = *input.Amount
		isModified = true
	}

	if input.Type != nil && *input.Type != existingSell.Type {
		newHistory = append(newHistory, models.SellHistory{
			Date:     now,
			Field:    "type",
			OldValue: existingSell.Type,
			NewValue: *input.Type,
		})
		updateFields["type"] = *input.Type
		isModified = true
	}

	if input.Comments != nil && *input.Comments != existingSell.Comments {
		newHistory = append(newHistory, models.SellHistory{
			Date:     now,
			Field:    "comments",
			OldValue: existingSell.Comments,
			NewValue: *input.Comments,
		})
		updateFields["comments"] = *input.Comments
		isModified = true
	}

	if !isModified {
		c.JSON(http.StatusOK, existingSell) // No changes
		return
	}

	updateFields["modified"] = true

	// Push new history items
	updateQuery := bson.M{
		"$set": updateFields,
	}
	if len(newHistory) > 0 {
		// Use $push with $each to append multiple items
		updateQuery["$push"] = bson.M{
			"history": bson.M{"$each": newHistory},
		}
	}

	_, err = database.SellsCollection.UpdateOne(ctx, bson.M{"_id": objID}, updateQuery)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al actualizar venta"})
		return
	}

	// Fetch updated document to return it
	var updatedSell models.Sell
	err = database.SellsCollection.FindOne(ctx, bson.M{"_id": objID}).Decode(&updatedSell)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "Venta actualizada", "warning": "No se pudo recuperar la venta actualizada"})
		return
	}

	c.JSON(http.StatusOK, updatedSell)
}

// CloseBoxHandler closes all open sells for the user (effectively closing the day)
func CloseBoxHandler(c *gin.Context) {
	userIDStr, exists := c.Get("userId")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario no identificado"})
		return
	}
	userID, _ := primitive.ObjectIDFromHex(userIDStr.(string))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Update all open sells for this user to Closed = true
	filter := bson.M{
		"userId":   userID,
		"isClosed": false,
	}
	update := bson.M{
		"$set": bson.M{"isClosed": true},
	}

	result, err := database.SellsCollection.UpdateMany(ctx, filter, update)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al cerrar caja"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "Caja cerrada exitosamente",
		"closedDetails": result.ModifiedCount,
	})
}
