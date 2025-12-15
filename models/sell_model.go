package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type SellType string

const (
	SellTypeCash     SellType = "Efectivo"
	SellTypeCredit   SellType = "Crédito"
	SellTypeDebit    SellType = "Débito"
	SellTypeTransfer SellType = "Transferencia"
)

type SellHistory struct {
	Date     time.Time   `bson:"date" json:"date"`
	Field    string      `bson:"field" json:"field"`
	OldValue interface{} `bson:"oldValue" json:"oldValue"`
	NewValue interface{} `bson:"newValue" json:"newValue"`
}

type Sell struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID   primitive.ObjectID `bson:"userId" json:"userId"`
	Amount   float64            `bson:"amount" json:"amount"`
	Date     time.Time          `bson:"date" json:"date"` // Creation date
	Type     SellType           `bson:"type" json:"type"`
	Comments string             `bson:"comments,omitempty" json:"comments,omitempty"`
	Modified bool               `bson:"modified" json:"modified"`
	History  []SellHistory      `bson:"history,omitempty" json:"history,omitempty"`
	IsClosed bool               `bson:"isClosed" json:"isClosed"` // True if the day/box is closed
}
