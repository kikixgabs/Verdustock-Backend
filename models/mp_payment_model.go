package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type MPPayment struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID      primitive.ObjectID `bson:"userId" json:"userId"`
	MPPaymentID int64              `bson:"mpPaymentId" json:"mpPaymentId"`
	Amount      float64            `bson:"amount" json:"amount"`
	PayerEmail  string             `bson:"payerEmail" json:"payerEmail"`

	// ✅ NUEVO CAMPO: Para guardar el nombre real (ej: "Maria Manera")
	PayerName string `bson:"payerName" json:"payerName"`

	Status      string    `bson:"status" json:"status"`
	ReceivedAt  time.Time `bson:"receivedAt" json:"receivedAt"`
	Source      string    `bson:"source" json:"source"`
	RawResponse string    `bson:"rawResponse" json:"rawResponse"`
}
