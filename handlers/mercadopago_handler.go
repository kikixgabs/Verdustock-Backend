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

// HandleMPWebhook processes incoming webhooks from Mercado Pago
func HandleMPWebhook(c *gin.Context) {
	// 1. Identify Target User via Query Param
	userIDStr := c.Query("userId")
	if userIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "userId query param required"})
		return
	}

	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid userId"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var user models.User
	err = database.UserCollection.FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	if user.MPAccount.AccessToken == "" {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "User has not configured MP Access Token"})
		return
	}

	// 2. Parse Webhook
	var req WebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid webhook payload"})
		return
	}

	if req.Type != "payment" {
		c.JSON(http.StatusOK, gin.H{"message": "Ignored non-payment event"})
		return
	}

	// 3. Query Mercado Pago API
	client := &http.Client{Timeout: 10 * time.Second}
	mpURL := fmt.Sprintf("https://api.mercadopago.com/v1/payments/%s", req.Data.ID)

	mpReq, err := http.NewRequest("GET", mpURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create MP request"})
		return
	}

	mpReq.Header.Set("Authorization", "Bearer "+user.MPAccount.AccessToken)

	resp, err := client.Do(mpReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to contact Mercado Pago"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Mercado Pago returned error"})
		return
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	var payment PaymentResponse
	if err := json.Unmarshal(bodyBytes, &payment); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse MP response"})
		return
	}

	// 4. Validate Logic (Approved + Account Money)
	if payment.Status != "approved" {
		c.JSON(http.StatusOK, gin.H{"message": "Payment not approved, ignored"})
		return
	}

	// Rule: payment_method_id must be "account_money" for transfers
	// Adjust this rule if credit card payments should also be accepted
	if payment.PaymentMethodID != "account_money" {
		c.JSON(http.StatusOK, gin.H{"message": "Payment method not account_money, ignored"})
		return
	}

	// 5. Save to MPPayments Collection (Canonical Record)
	receivedAt, _ := time.Parse(time.RFC3339, payment.DateCreated) // MP Format usually RFC3339

	mpPayment := models.MPPayment{
		ID:          primitive.NewObjectID(),
		UserID:      userID,
		MPPaymentID: payment.ID,
		Amount:      payment.TransactionAmount,
		PayerEmail:  payment.Payer.Email,
		Status:      payment.Status,
		ReceivedAt:  receivedAt,
		Source:      "TRANSFER", // Derived from account_money logic
		RawResponse: string(bodyBytes),
	}

	// Check if already exists to avoid duplicates
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

	// 6. Create Sell (Business Logic)
	sell := models.Sell{
		ID:       primitive.NewObjectID(),
		UserID:   userID,
		Amount:   payment.TransactionAmount,
		Date:     time.Now(),
		Type:     models.SellTypeTransfer,
		Comments: fmt.Sprintf("MP Transfer ID: %d from %s", payment.ID, payment.Payer.Email),
		Modified: false,
		IsClosed: false,
		History:  []models.SellHistory{},
	}

	_, err = database.SellsCollection.InsertOne(ctx, sell)
	if err != nil {
		// Log error but payment was recorded
		fmt.Printf("Error creating sell for payment %d: %v\n", payment.ID, err)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Payment processed and sell created"})
}

// LinkMPAccountHandler links a Mercado Pago account to the user
func LinkMPAccountHandler(c *gin.Context) {
	// Get UserID from context
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
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		UserID       int64  `json:"mpUserId"` // MP User ID
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data"})
		return
	}

	if input.AccessToken == "" || input.RefreshToken == "" || input.UserID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required MP credentials"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	update := bson.M{
		"$set": bson.M{
			"mpAccount": models.MPAccount{
				AccessToken:  input.AccessToken,
				RefreshToken: input.RefreshToken,
				UserID:       input.UserID,
			},
			"mpAccountConnected": true,
		},
	}

	result, err := database.UserCollection.UpdateOne(ctx, bson.M{"_id": userID}, update)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to link MP account"})
		return
	}

	if result.MatchedCount == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Mercado Pago account linked successfully"})
}
