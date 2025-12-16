package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type MPPayment struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID      primitive.ObjectID `bson:"userId" json:"userId"` // Which user received this
	MPPaymentID int64              `bson:"mpPaymentId" json:"mpPaymentId"`
	Amount      float64            `bson:"amount" json:"amount"`
	PayerEmail  string             `bson:"payerEmail" json:"payerEmail"`
	Status      string             `bson:"status" json:"status"`
	ReceivedAt  time.Time          `bson:"receivedAt" json:"receivedAt"`
	Source      string             `bson:"source" json:"source"`                               // e.g. "TRANSFER"
	RawResponse string             `bson:"rawResponse,omitempty" json:"rawResponse,omitempty"` // Debugging
}
