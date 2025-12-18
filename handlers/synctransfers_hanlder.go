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

	// 2. CONFIGURACIÓN: Traer últimos 3 días (Buffer de seguridad)
	// No filtramos por fecha exacta aquí, dejamos que entre todo lo reciente.
	endTime := time.Now()
	startTime := endTime.Add(-72 * time.Hour)

	beginDateISO := startTime.Format(time.RFC3339)
	endDateISO := endTime.Format(time.RFC3339)

	baseURL := "https://api.mercadopago.com/v1/payments/search"

	params := url.Values{}
	params.Add("status", "approved")
	params.Add("sort", "date_created")
	params.Add("criteria", "desc")
	params.Add("limit", "100")

	// ⚠️ QUITAMOS 'payment_type_id' porque estaba ocultando transferencias reales.
	// Usaremos filtros de texto manuales más abajo.
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

	bodyBytes, _ := io.ReadAll(resp.Body)
	var searchResult MPSearchResponse
	if err := json.Unmarshal(bodyBytes, &searchResult); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error leyendo respuesta de MP"})
		return
	}

	newCount := 0

	for _, payment := range searchResult.Results {

		// 🛑 FILTRO ANTI-GASTOS (HBO, Paramount, etc.)
		// Si la descripción contiene palabras clave de gastos, LO IGNORAMOS.
		desc := strings.ToLower(payment.Description)
		if strings.Contains(desc, "paramount") ||
			strings.Contains(desc, "hbo") ||
			strings.Contains(desc, "netflix") ||
			strings.Contains(desc, "suscripción") ||
			strings.Contains(desc, "spotify") {
			// fmt.Printf("🗑️ Ignorando gasto personal: %s\n", payment.Description)
			continue
		}

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

		// Fecha Real
		realDate, _ := time.Parse(time.RFC3339, payment.DateCreated)

		mpPayment := models.MPPayment{
			ID:          primitive.NewObjectID(),
			UserID:      user.ID,
			MPPaymentID: payment.ID,
			Amount:      payment.TransactionAmount,
			PayerEmail:  payment.Payer.Email,
			PayerName:   finalName,
			Status:      payment.Status,
			ReceivedAt:  realDate,
			Source:      "SYNC_CVU",
			RawResponse: "",
		}

		database.MPPaymentsCollection.InsertOne(ctx, mpPayment)

		// Crear Venta
		sell := models.Sell{
			ID:       primitive.NewObjectID(),
			UserID:   user.ID,
			Amount:   payment.TransactionAmount,
			Date:     realDate, // Guardamos fecha real
			Type:     "Transferencia",
			Comments: fmt.Sprintf("%s (#%d)", finalName, payment.ID),
			Modified: false,
			IsClosed: false,
			History:  []models.SellHistory{},
		}
		database.SellsCollection.InsertOne(ctx, sell)

		newCount++
		fmt.Printf("✅ Guardado: %s - %s ($%.2f)\n", payment.DateCreated, finalName, payment.TransactionAmount)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Sincronización completada. %d transferencias procesadas.", newCount),
		"new":     newCount,
	})
}
