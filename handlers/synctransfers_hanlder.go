package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"verdustock-auth/database"
	"verdustock-auth/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Estructura para la respuesta de búsqueda de MP
type MPSearchResponse struct {
	Results []PaymentResponse `json:"results"`
}

func SyncMPTransfersHandler(c *gin.Context) {
	// 1. Obtener usuario autenticado
	userIDStr, exists := c.Get("userId")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	userID, _ := primitive.ObjectIDFromHex(userIDStr.(string))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 2. Obtener Token del usuario
	var user models.User
	err := database.UserCollection.FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
	if err != nil || user.MPAccount == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Usuario no tiene Mercado Pago vinculado"})
		return
	}

	// 3. Consultar a Mercado Pago: "Dame las últimas transferencias aprobadas"
	client := &http.Client{Timeout: 10 * time.Second}

	// Filtros CLAVE: status=approved Y payment_type_id=bank_transfer
	mpURL := "https://api.mercadopago.com/v1/payments/search?status=approved&payment_type_id=bank_transfer&sort=date_created&criteria=desc&limit=20"

	req, _ := http.NewRequest("GET", mpURL, nil)
	req.Header.Set("Authorization", "Bearer "+user.MPAccount.AccessToken)

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Error conectando con MP"})
		return
	}
	defer resp.Body.Close()

	var searchResult MPSearchResponse
	bodyBytes, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(bodyBytes, &searchResult); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error leyendo respuesta de MP"})
		return
	}

	newCount := 0

	// 4. Procesar resultados y guardar solo los NUEVOS
	for _, payment := range searchResult.Results {

		// Verificar si ya existe en nuestra DB
		count, _ := database.MPPaymentsCollection.CountDocuments(ctx, bson.M{"mpPaymentId": payment.ID})
		if count > 0 {
			continue // Ya lo tenemos, saltar
		}

		// Si es nuevo, lo guardamos (Misma lógica que el Webhook)
		receivedAt, _ := time.Parse(time.RFC3339, payment.DateCreated)

		mpPayment := models.MPPayment{
			ID:          primitive.NewObjectID(),
			UserID:      user.ID,
			MPPaymentID: payment.ID,
			Amount:      payment.TransactionAmount,
			PayerEmail:  payment.Payer.Email,
			Status:      payment.Status,
			ReceivedAt:  receivedAt,
			Source:      "SYNC_CVU", // Marcamos que vino por Sincronización
			RawResponse: "",
		}

		database.MPPaymentsCollection.InsertOne(ctx, mpPayment)

		// Crear Venta
		sell := models.Sell{
			ID:       primitive.NewObjectID(),
			UserID:   user.ID,
			Amount:   payment.TransactionAmount,
			Date:     time.Now(),
			Type:     "transfer",
			Comments: fmt.Sprintf("Transferencia CVU detectada #%d", payment.ID),
			Modified: false,
			IsClosed: false,
			History:  []models.SellHistory{},
		}
		database.SellsCollection.InsertOne(ctx, sell)

		newCount++
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Sincronización completada. %d nuevas transferencias detectadas.", newCount),
		"new":     newCount,
	})
}
