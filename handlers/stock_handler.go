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
)

// InitializeCatalog checks if the catalog is empty and populates it if so
func InitializeCatalog() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	count, err := database.CatalogCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return err
	}

	if count == 0 {
		defaultProducts := models.GetDefaultProducts()
		var documents []interface{}
		for _, p := range defaultProducts {
			// Catalog items don't need UserID
			p.ID = primitive.NewObjectID()
			documents = append(documents, p)
		}

		if len(documents) > 0 {
			_, err := database.CatalogCollection.InsertMany(ctx, documents)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// GetStockHandler returns the stock for the logged in user
func GetStockHandler(c *gin.Context) {
	// Get UserID from context (set by middleware)
	userIDStr, exists := c.Get("userId")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario no identificado"})
		return
	}

	userID, err := primitive.ObjectIDFromHex(userIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID de usuario inválido"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Find products for this user
	cursor, err := database.StockCollection.Find(ctx, bson.M{"userId": userID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al obtener stock"})
		return
	}

	var products []models.Product
	if err = cursor.All(ctx, &products); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al decodificar productos"})
		return
	}

	// If user has no products, initialize them from the CATALOG collection
	if len(products) == 0 {
		// Fetch from Catalog
		catalogCursor, err := database.CatalogCollection.Find(ctx, bson.M{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al obtener catálogo"})
			return
		}
		defer catalogCursor.Close(ctx)

		var catalogItems []models.Product
		if err = catalogCursor.All(ctx, &catalogItems); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al leer catálogo"})
			return
		}

		var documents []interface{}

		for _, p := range catalogItems {
			p.ID = primitive.NewObjectID()
			p.UserID = userID
			p.Stock = 0 // Ensure starts at 0
			// Append both to the slice of documents to insert and the slice to return
			documents = append(documents, p)
			products = append(products, p)
		}

		if len(documents) > 0 {
			_, err := database.StockCollection.InsertMany(ctx, documents)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al inicializar productos"})
				return
			}
		}
	}

	c.JSON(http.StatusOK, products)
}

// UpdateProductHandler updates stock and measurement for a specific product
func UpdateProductHandler(c *gin.Context) {
	idStr := c.Param("id")
	objID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID de producto inválido"})
		return
	}

	userIDStr, exists := c.Get("userId")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario no identificado"})
		return
	}
	userID, _ := primitive.ObjectIDFromHex(userIDStr.(string))

	var input struct {
		Stock       *float64            `json:"stock"`
		Measurement *models.Measurement `json:"measurement"`
		Loaded      *bool               `json:"loaded"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Datos inválidos"})
		return
	}

	update := bson.M{}
	if input.Stock != nil {
		update["stock"] = *input.Stock
	}
	if input.Measurement != nil {
		update["measurement"] = *input.Measurement
	}
	if input.Loaded != nil {
		update["loaded"] = *input.Loaded
	}

	if len(update) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No se enviaron datos para actualizar"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := database.StockCollection.UpdateOne(
		ctx,
		bson.M{"_id": objID, "userId": userID},
		bson.M{"$set": update},
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al actualizar producto"})
		return
	}

	if result.MatchedCount == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Producto no encontrado o no pertenece al usuario"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Producto actualizado correctamente"})
}

// CreateProductHandler allows creating a new product
func CreateProductHandler(c *gin.Context) {
	userIDStr, exists := c.Get("userId")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario no identificado"})
		return
	}
	userID, _ := primitive.ObjectIDFromHex(userIDStr.(string))

	var product models.Product
	if err := c.ShouldBindJSON(&product); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Datos inválidos"})
		return
	}

	// Assign current user ID and new ObjectID
	product.UserID = userID
	product.ID = primitive.NewObjectID()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := database.StockCollection.InsertOne(ctx, product)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al crear producto"})
		return
	}

	c.JSON(http.StatusCreated, product)
}
