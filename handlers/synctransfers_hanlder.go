package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	// 🛑 ESTRATEGIA NUCLEAR: SIN FECHAS ☢️
	// Eliminamos range, begin_date y end_date.
	// Pedimos simplemente los últimos 50 pagos aprobados de la historia de esta cuenta.

	baseURL := "https://api.mercadopago.com/v1/payments/search"

	// Construcción manual simple para evitar errores de codificación
	// sort=date_created&criteria=desc -> Trae los más nuevos primero
	finalURL := fmt.Sprintf("%s?status=approved&sort=date_created&criteria=desc&limit=50", baseURL)

	// LOG DE DEBUG IMPORTANTE:
	// Muestra los últimos 5 caracteres del Token para que verifiques si es la cuenta correcta
	token := user.MPAccount.AccessToken
	maskedToken := "..."
	if len(token) > 5 {
		maskedToken = token[len(token)-5:]
	}
	fmt.Printf("🔍 Sincronizando Cuenta (Token termina en: %s)\n", maskedToken)
	fmt.Printf("🌍 URL: %s\n", finalURL)

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", finalURL, nil)
	req.Header.Set("Authorization", "Bearer "+user.MPAccount.AccessToken)

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Error conectando con MP"})
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	// LOG PARA VER SI MP DEVUELVE ALGO VACÍO
	respStr := string(bodyBytes)
	if len(respStr) > 500 {
		fmt.Printf("📦 Respuesta MP (RAW): %s... \n", respStr[:500])
	} else {
		fmt.Printf("📦 Respuesta MP (RAW): %s \n", respStr)
	}

	var searchResult MPSearchResponse
	if err := json.Unmarshal(bodyBytes, &searchResult); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error leyendo respuesta de MP"})
		return
	}

	newCount := 0

	// 4. Procesar resultados
	for _, payment := range searchResult.Results {

		// Verificar duplicados (ID Pago + ID Usuario)
		count, _ := database.MPPaymentsCollection.CountDocuments(ctx, bson.M{
			"mpPaymentId": payment.ID,
			"userId":      user.ID,
		})

		if count > 0 {
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

		// Crear Venta
		sell := models.Sell{
			ID:       primitive.NewObjectID(),
			UserID:   user.ID,
			Amount:   payment.TransactionAmount,
			Date:     time.Now(), // Usamos fecha actual para que aparezca arriba en la lista
			Type:     "Transferencia",
			Comments: fmt.Sprintf("%s (#%d)", finalName, payment.ID),
			Modified: false,
			IsClosed: false,
			History:  []models.SellHistory{},
		}
		database.SellsCollection.InsertOne(ctx, sell)

		newCount++
		fmt.Printf("✅ RECUPERADO: %d - %s ($%.2f)\n", payment.ID, finalName, payment.TransactionAmount)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Sincronización Nuclear completada. %d pagos recuperados.", newCount),
		"new":     newCount,
	})
}
