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

	// 2. ESTRATEGIA: "RED DE PESCA GRANDE" (Últimos 3 días) 🎣
	// Volvemos a mirar atrás para recuperar las transferencias de ayer (Dec 17)

	loc := time.FixedZone("ART", -3*60*60)
	endTime := time.Now().In(loc)
	startTime := endTime.Add(-72 * time.Hour) // <-- CLAVE: Miramos 3 días atrás

	beginDateISO := startTime.Format(time.RFC3339)
	endDateISO := endTime.Format(time.RFC3339)

	// 3. Construir URL
	baseURL := "https://api.mercadopago.com/v1/payments/search"

	params := url.Values{}
	params.Add("status", "approved")
	params.Add("sort", "date_created")
	params.Add("criteria", "desc")
	params.Add("limit", "100")

	// Filtros de fecha AMPLIOS
	params.Add("range", "date_created")
	params.Add("begin_date", beginDateISO)
	params.Add("end_date", endDateISO)

	finalURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	// Debug
	fmt.Printf("🔍 Sincronizando (Desde %s hasta %s)\n", beginDateISO, endDateISO)

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
			Date:     time.Now(),      // Usamos fecha actual para que entre en la caja de "Hoy"
			Type:     "Transferencia", // ✅ Corregido a Mayúscula
			Comments: fmt.Sprintf("%s (#%d)", finalName, payment.ID),
			Modified: false,
			IsClosed: false,
			History:  []models.SellHistory{},
		}
		database.SellsCollection.InsertOne(ctx, sell)

		newCount++
		fmt.Printf("✅ Guardado: %d - %s ($%.2f)\n", payment.ID, finalName, payment.TransactionAmount)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Sincronización completada. %d nuevas transferencias recuperadas.", newCount),
		"new":     newCount,
	})
}
