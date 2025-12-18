package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings" // Agregamos strings para limpiar texto
	"time"

	"verdustock-auth/database"
	"verdustock-auth/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Definimos una estructura EXTENDIDA solo para este archivo.
// Esto asegura que leamos los campos de nombre sin romper el otro archivo.
type ExtendedPaymentResponse struct {
	ID                int64   `json:"id"`
	Status            string  `json:"status"`
	TransactionAmount float64 `json:"transaction_amount"`
	DateCreated       string  `json:"date_created"`
	Description       string  `json:"description"` // ✅ Aquí suele venir el nombre en transferencias
	Payer             struct {
		Email     string `json:"email"`
		FirstName string `json:"first_name"` // ✅ Leemos nombre
		LastName  string `json:"last_name"`  // ✅ Leemos apellido
	} `json:"payer"`
}

// Estructura para la respuesta de búsqueda de MP
type MPSearchResponse struct {
	Results []ExtendedPaymentResponse `json:"results"` // Usamos la estructura extendida
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

	// 3. Consultar a Mercado Pago
	client := &http.Client{Timeout: 10 * time.Second}

	// Filtros: approved y bank_transfer
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

	// 4. Procesar resultados
	for _, payment := range searchResult.Results {

		// Verificar si ya existe en nuestra DB
		count, _ := database.MPPaymentsCollection.CountDocuments(ctx, bson.M{"mpPaymentId": payment.ID})
		if count > 0 {
			continue // Ya lo tenemos, saltar
		}

		// --- LÓGICA DE DETECCIÓN DE NOMBRE ---
		finalName := "Desconocido"

		// Intento A: Nombre y Apellido del objeto Payer
		if payment.Payer.FirstName != "" || payment.Payer.LastName != "" {
			finalName = strings.TrimSpace(fmt.Sprintf("%s %s", payment.Payer.FirstName, payment.Payer.LastName))
		}

		// Intento B: Si falló A, usar la Descripción (Ej: "Transferencia de Maria...")
		if finalName == "Desconocido" || finalName == "" {
			if payment.Description != "" && payment.Description != "null" {
				finalName = payment.Description
			}
		}

		// Intento C: Si todo falló, usar Email
		if finalName == "Desconocido" || finalName == "" {
			finalName = payment.Payer.Email
		}
		// -------------------------------------

		receivedAt, _ := time.Parse(time.RFC3339, payment.DateCreated)

		mpPayment := models.MPPayment{
			ID:          primitive.NewObjectID(),
			UserID:      user.ID,
			MPPaymentID: payment.ID,
			Amount:      payment.TransactionAmount,
			PayerEmail:  payment.Payer.Email,
			PayerName:   finalName, // ✅ Guardamos el nombre detectado
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
			Date:     time.Now(),
			Type:     "transfer",
			Comments: fmt.Sprintf("%s (#%d)", finalName, payment.ID), // ✅ Nombre en comentario
			Modified: false,
			IsClosed: false,
			History:  []models.SellHistory{},
		}
		database.SellsCollection.InsertOne(ctx, sell)

		newCount++
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Sincronización completada. %d nuevas transferencias.", newCount),
		"new":     newCount,
	})
}
