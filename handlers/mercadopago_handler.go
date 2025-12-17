package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"verdustock-auth/database"
	"verdustock-auth/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Estructura actualizada para leer el user_id que manda MP
type WebhookRequest struct {
	Type   string `json:"type"`
	Action string `json:"action"`
	Data   struct {
		ID string `json:"id"`
	} `json:"data"`
	UserID int64 `json:"user_id"` // <--- EL DATO CLAVE QUE FALTABA
}

type PaymentResponse struct {
	ID                int64   `json:"id"`
	Status            string  `json:"status"`
	TransactionAmount float64 `json:"transaction_amount"`
	DateCreated       string  `json:"date_created"`
	PaymentMethodID   string  `json:"payment_method_id"`
	Payer             struct {
		Email string `json:"email"`
	} `json:"payer"`
}

type MPOAuthResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	UserID       int64  `json:"user_id"`
	RefreshToken string `json:"refresh_token"`
	PublicKey    string `json:"public_key"`
}

// ==========================================
// 1. LINK ACCOUNT HANDLER (Igual que antes)
// ==========================================
func LinkMPAccountHandler(c *gin.Context) {
	// ... (Tu código de LinkMPAccountHandler estaba bien, déjalo igual o cópialo si quieres asegurarte)
	userIDStr, exists := c.Get("userId")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	userID, err := primitive.ObjectIDFromHex(userIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid User ID"})
		return
	}

	var input struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization code is required"})
		return
	}

	clientID := os.Getenv("MP_CLIENT_ID")
	clientSecret := os.Getenv("MP_CLIENT_SECRET")
	redirectURI := os.Getenv("MP_REDIRECT_URI")

	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("grant_type", "authorization_code")
	data.Set("code", input.Code)
	data.Set("redirect_uri", redirectURI)

	resp, err := http.PostForm("https://api.mercadopago.com/oauth/token", data)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to Mercado Pago"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ Error MP OAuth: %s\n", string(bodyBytes))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mercado Pago rejected the authorization code"})
		return
	}

	var mpData MPOAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&mpData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse MP response"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mpAccount := models.MPAccount{
		AccessToken:  mpData.AccessToken,
		RefreshToken: mpData.RefreshToken,
		UserID:       mpData.UserID,
		PublicKey:    mpData.PublicKey,
		ExpiresIn:    mpData.ExpiresIn,
		UpdatedAt:    time.Now(),
	}

	update := bson.M{
		"$set": bson.M{
			"mpAccount":          mpAccount,
			"mpAccountConnected": true,
		},
	}

	_, err = database.UserCollection.UpdateOne(ctx, bson.M{"_id": userID}, update)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save MP credentials"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Mercado Pago account linked successfully!"})
}

// ==========================================
// 2. WEBHOOK HANDLER (¡CORREGIDO!)
// ==========================================
func HandleMPWebhook(c *gin.Context) {
	// 1. Parsear el Webhook (Ahora leemos UserID del JSON)
	var req WebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Payload inválido"})
		return
	}

	// Logs para debug en Render
	fmt.Printf("🔔 Webhook Recibido - Type: %s, Action: %s, UserID: %d, DataID: %s\n", req.Type, req.Action, req.UserID, req.Data.ID)

	// Solo nos interesan los eventos de pago
	if req.Type != "payment" && req.Action != "payment.created" && req.Action != "payment.updated" {
		c.JSON(http.StatusOK, gin.H{"message": "Evento ignorado"})
		return
	}

	// 2. BUSCAR AL USUARIO POR SU ID DE MERCADO PAGO
	// Aquí está la magia: Buscamos quién tiene este ID vinculado en Mongo
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var user models.User
	err := database.UserCollection.FindOne(ctx, bson.M{"mpAccount.userId": req.UserID}).Decode(&user)

	if err != nil {
		// Si no encontramos al usuario, respondemos 200 (OK) para que MP no reintente,
		// pero logueamos el error.
		fmt.Printf("⚠️ Usuario no encontrado para MP User ID: %d\n", req.UserID)
		c.JSON(http.StatusOK, gin.H{"message": "Usuario no encontrado, evento ignorado"})
		return
	}

	// 3. Consultar API de Mercado Pago
	client := &http.Client{Timeout: 10 * time.Second}
	mpURL := fmt.Sprintf("https://api.mercadopago.com/v1/payments/%s", req.Data.ID)

	mpReq, err := http.NewRequest("GET", mpURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creando request"})
		return
	}

	mpReq.Header.Set("Authorization", "Bearer "+user.MPAccount.AccessToken)

	resp, err := client.Do(mpReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Error contactando a MP"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Esto pasará con el ID falso "123456789" del simulador
		fmt.Println("❌ Mercado Pago devolvió error al consultar el pago (Probablemente ID falso)")
		c.JSON(http.StatusOK, gin.H{"error": "No se pudo obtener detalle del pago"})
		return
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	var payment PaymentResponse
	if err := json.Unmarshal(bodyBytes, &payment); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error parseando pago"})
		return
	}

	// 4. Validar que sea Aprobado
	if payment.Status != "approved" {
		c.JSON(http.StatusOK, gin.H{"message": "Pago no aprobado, ignorado"})
		return
	}

	// 5. Evitar Duplicados
	count, _ := database.MPPaymentsCollection.CountDocuments(ctx, bson.M{"mpPaymentId": payment.ID})
	if count > 0 {
		c.JSON(http.StatusOK, gin.H{"message": "El pago ya fue procesado anteriormente"})
		return
	}

	// 6. Guardar el Registro Crudo
	receivedAt, err := time.Parse(time.RFC3339, payment.DateCreated)
	if err != nil {
		receivedAt = time.Now()
	}

	mpPayment := models.MPPayment{
		ID:          primitive.NewObjectID(),
		UserID:      user.ID, // Usamos el ID del usuario que encontramos en la DB
		MPPaymentID: payment.ID,
		Amount:      payment.TransactionAmount,
		PayerEmail:  payment.Payer.Email,
		Status:      payment.Status,
		ReceivedAt:  receivedAt,
		Source:      "TRANSFER",
		RawResponse: string(bodyBytes),
	}

	_, err = database.MPPaymentsCollection.InsertOne(ctx, mpPayment)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error guardando pago"})
		return
	}

	// 7. Crear la Venta
	sell := models.Sell{
		ID:       primitive.NewObjectID(),
		UserID:   user.ID,
		Amount:   payment.TransactionAmount,
		Date:     time.Now(),
		Type:     "transfer",
		Comments: fmt.Sprintf("MP Transfer #%d - %s", payment.ID, payment.Payer.Email),
		Modified: false,
		IsClosed: false,
		History:  []models.SellHistory{},
	}

	_, err = database.SellsCollection.InsertOne(ctx, sell)
	if err != nil {
		fmt.Printf("⚠️ Error creando venta: %v\n", err)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Pago procesado exitosamente"})
}
