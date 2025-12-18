package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"verdustock-auth/database"
	"verdustock-auth/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Estructura EXTENDIDA
type ExtendedPaymentResponse struct {
	ID                int64   `json:"id"`
	Status            string  `json:"status"`
	TransactionAmount float64 `json:"transaction_amount"`
	DateCreated       string  `json:"date_created"`
	Description       string  `json:"description"`
	Payer             struct {
		Email     string `json:"email"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	} `json:"payer"`
}

type MPSearchResponse struct {
	Results []ExtendedPaymentResponse `json:"results"`
}

func SyncMPTransfersHandler(c *gin.Context) {
	// 1. Obtener usuario
	userIDStr, exists := c.Get("userId")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	userID, _ := primitive.ObjectIDFromHex(userIDStr.(string))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var user models.User
	err := database.UserCollection.FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
	if err != nil || user.MPAccount == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Usuario no tiene Mercado Pago vinculado"})
		return
	}

	// 2. ESTRATEGIA: "RED DE PESCA GRANDE" (Últimos 3 días)
	endTime := time.Now()
	startTime := endTime.Add(-72 * time.Hour)

	beginDateISO := startTime.Format(time.RFC3339)
	endDateISO := endTime.Format(time.RFC3339)

	// 3. Construir URL
	baseURL := "https://api.mercadopago.com/v1/payments/search"

	params := url.Values{}
	params.Add("status", "approved")
	params.Add("sort", "date_created")
	params.Add("criteria", "desc")
	params.Add("limit", "50")

	// NOTA: Mantenemos el filtro de tipo desactivado para asegurar que entra todo
	// params.Add("payment_type_id", "bank_transfer")

	params.Add("range", "date_created")
	params.Add("begin_date", beginDateISO)
	params.Add("end_date", endDateISO)

	finalURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	fmt.Printf("🔍 Sincronizando: %s\n", finalURL)

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", finalURL, nil)
	req.Header.Set("Authorization", "Bearer "+user.MPAccount.AccessToken)

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Error conectando con MP"})
		return
	}
	defer resp.Body.Close()

	var searchResult MPSearchResponse
	bodyBytes, _ := io.ReadAll(resp.Body)

	// Debug logs
	respString := string(bodyBytes)
	if len(respString) > 200 {
		fmt.Printf("📦 Respuesta MP: %s...\n", respString[:200])
	} else {
		fmt.Printf("📦 Respuesta MP: %s\n", respString)
	}

	if err := json.Unmarshal(bodyBytes, &searchResult); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error leyendo respuesta de MP"})
		return
	}

	newCount := 0

	// 4. Procesar resultados
	for _, payment := range searchResult.Results {

		// ✅✅✅ AQUÍ ESTÁ EL CAMBIO CRÍTICO ✅✅✅
		// Ahora verificamos ID del Pago Y ID del Usuario al mismo tiempo.
		count, _ := database.MPPaymentsCollection.CountDocuments(ctx, bson.M{
			"mpPaymentId": payment.ID,
			"userId":      user.ID, // <--- ESTA LÍNEA SOLUCIONA EL "FANTASMA"
		})

		if count > 0 {
			// Si ESTE usuario ya tiene el pago, lo saltamos.
			continue
		}

		// Lógica de Nombre
		finalName := "Desconocido"
		if payment.Payer.FirstName != "" || payment.Payer.LastName != "" {
			finalName = strings.TrimSpace(fmt.Sprintf("%s %s", payment.Payer.FirstName, payment.Payer.LastName))
		}
		if finalName == "Desconocido" || finalName == "" {
			if payment.Description != "" && payment.Description != "null" {
				finalName = payment.Description
			}
		}
		if finalName == "Desconocido" || finalName == "" {
			finalName = payment.Payer.Email
		}

		receivedAt, _ := time.Parse(time.RFC3339, payment.DateCreated)

		mpPayment := models.MPPayment{
			ID:          primitive.NewObjectID(),
			UserID:      user.ID,
			MPPaymentID: payment.ID,
			Amount:      payment.TransactionAmount,
			PayerEmail:  payment.Payer.Email,
			PayerName:   finalName,
			Status:      payment.Status,
			ReceivedAt:  receivedAt,
			Source:      "SYNC_CVU",
			RawResponse: "",
		}

		database.MPPaymentsCollection.InsertOne(ctx, mpPayment)

		sell := models.Sell{
			ID:       primitive.NewObjectID(),
			UserID:   user.ID,
			Amount:   payment.TransactionAmount,
			Date:     time.Now(),
			Type:     "transfer",
			Comments: fmt.Sprintf("%s (#%d)", finalName, payment.ID),
			Modified: false,
			IsClosed: false,
			History:  []models.SellHistory{},
		}
		database.SellsCollection.InsertOne(ctx, sell)

		newCount++
		fmt.Printf("✅ Guardado nuevo pago: %d - %s\n", payment.ID, finalName)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Sincronización completada. %d nuevas transferencias.", newCount),
		"new":     newCount,
	})
}
