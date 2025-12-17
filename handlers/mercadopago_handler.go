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

// Estructuras auxiliares
type WebhookRequest struct {
	Type string `json:"type"`
	Data struct {
		ID string `json:"id"`
	} `json:"data"`
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

// Estructura para la respuesta de OAuth de Mercado Pago
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
// 1. LINK ACCOUNT HANDLER (Corregido OAuth)
// ==========================================
func LinkMPAccountHandler(c *gin.Context) {
	// Obtener ID del usuario logueado
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

	// Recibimos SOLO el "code" del frontend
	var input struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization code is required"})
		return
	}

	// Preparamos la petición a Mercado Pago para canjear el código
	clientID := os.Getenv("MP_CLIENT_ID")
	clientSecret := os.Getenv("MP_CLIENT_SECRET")
	redirectURI := os.Getenv("MP_REDIRECT_URI")

	// Formulario POST
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("grant_type", "authorization_code")
	data.Set("code", input.Code)
	data.Set("redirect_uri", redirectURI)

	// Hacemos el POST a MP
	resp, err := http.PostForm("https://api.mercadopago.com/oauth/token", data)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to Mercado Pago"})
		return
	}
	defer resp.Body.Close()

	// Si MP nos rechaza (ej: código viejo o inválido)
	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ Error MP OAuth: %s\n", string(bodyBytes))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mercado Pago rejected the authorization code"})
		return
	}

	// Decodificamos los tokens
	var mpData MPOAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&mpData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse MP response"})
		return
	}

	// Guardamos en la Base de Datos
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Construimos el objeto MPAccount
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
// 2. WEBHOOK HANDLER
// ==========================================
func HandleMPWebhook(c *gin.Context) {
	// 1. Identificar Usuario via Query Param (?userId=...)
	userIDStr := c.Query("userId")
	if userIDStr == "" {
		// Importante: Responder 200 a MP aunque falte el ID para que no reintenten infinitamente
		c.JSON(http.StatusOK, gin.H{"error": "userId query param missing (ignored)"})
		return
	}

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": "Invalid userId (ignored)"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Buscar usuario para obtener su Access Token
	var user models.User
	err = database.UserCollection.FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": "User not found (ignored)"})
		return
	}

	// Verificamos que tenga cuenta vinculada y token
	if user.MPAccount == nil || user.MPAccount.AccessToken == "" {
		c.JSON(http.StatusOK, gin.H{"error": "User has not configured MP Access Token"})
		return
	}

	// 2. Parsear Webhook
	var req WebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid webhook payload"})
		return
	}

	// Solo nos interesan pagos
	if req.Type != "payment" {
		c.JSON(http.StatusOK, gin.H{"message": "Ignored non-payment event"})
		return
	}

	// 3. Consultar API de Mercado Pago (Validación real)
	client := &http.Client{Timeout: 10 * time.Second}
	mpURL := fmt.Sprintf("https://api.mercadopago.com/v1/payments/%s", req.Data.ID)

	mpReq, err := http.NewRequest("GET", mpURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create MP request"})
		return
	}

	// Usamos el token del Vendedor (Usuario)
	mpReq.Header.Set("Authorization", "Bearer "+user.MPAccount.AccessToken)

	resp, err := client.Do(mpReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to contact Mercado Pago"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusOK, gin.H{"error": "Mercado Pago returned error or payment not found"})
		return
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	var payment PaymentResponse
	if err := json.Unmarshal(bodyBytes, &payment); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse MP response"})
		return
	}

	// 4. Lógica de Negocio (Solo Aprobados y Dinero en Cuenta/Transferencia)
	if payment.Status != "approved" {
		c.JSON(http.StatusOK, gin.H{"message": "Payment not approved, ignored"})
		return
	}

	// Filtro opcional: Solo aceptar transferencias (account_money)
	// Si quieres aceptar tarjeta de débito/crédito, comenta este if.
	if payment.PaymentMethodID != "account_money" {
		// c.JSON(http.StatusOK, gin.H{"message": "Payment method not account_money, ignored"})
		// return
	}

	// 5. Guardar en Colección MPPayments (Historial crudo)
	// Parsear fecha con seguridad
	receivedAt, err := time.Parse(time.RFC3339, payment.DateCreated)
	if err != nil {
		receivedAt = time.Now() // Fallback si la fecha viene rara
	}

	mpPayment := models.MPPayment{
		ID:          primitive.NewObjectID(),
		UserID:      userID,
		MPPaymentID: payment.ID,
		Amount:      payment.TransactionAmount,
		PayerEmail:  payment.Payer.Email,
		Status:      payment.Status,
		ReceivedAt:  receivedAt,
		Source:      "TRANSFER",
		RawResponse: string(bodyBytes),
	}

	// Evitar duplicados (Idempotencia)
	count, _ := database.MPPaymentsCollection.CountDocuments(ctx, bson.M{"mpPaymentId": payment.ID})
	if count > 0 {
		c.JSON(http.StatusOK, gin.H{"message": "Payment already processed"})
		return
	}

	_, err = database.MPPaymentsCollection.InsertOne(ctx, mpPayment)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save payment record"})
		return
	}

	// 6. Crear Venta (Sell) en el sistema
	sell := models.Sell{
		ID:       primitive.NewObjectID(),
		UserID:   userID,
		Amount:   payment.TransactionAmount,
		Date:     time.Now(),
		Type:     "transfer", // Asegúrate de que este string coincida con tu frontend
		Comments: fmt.Sprintf("Transferencia MP #%d de %s", payment.ID, payment.Payer.Email),
		Modified: false,
		IsClosed: false,
		History:  []models.SellHistory{},
	}

	_, err = database.SellsCollection.InsertOne(ctx, sell)
	if err != nil {
		fmt.Printf("⚠️ Error creating sell for payment %d: %v\n", payment.ID, err)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Payment processed and sell created"})
}
