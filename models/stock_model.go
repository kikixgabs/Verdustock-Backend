package models

import "go.mongodb.org/mongo-driver/bson/primitive"

type ProductType string
type Measurement string

const (
	Fruit     ProductType = "FRUTA"
	Vegetable ProductType = "VEGETAL"
	Ortaliza  ProductType = "ORTALIZA"
	Other     ProductType = "OTROS"
)

const (
	Unidades Measurement = "UNIDADES"
	Kilos    Measurement = "KILOS"
	Cajones  Measurement = "CAJONES"
	Bolsas   Measurement = "BOLSAS"
)

type Product struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID      primitive.ObjectID `bson:"userId" json:"userId"`
	Name        string             `bson:"name" json:"name"`
	Stock       float64            `bson:"stock" json:"stock"`
	Type        ProductType        `bson:"type,omitempty" json:"type,omitempty"`
	Measurement Measurement        `bson:"measurement" json:"measurement"`
	Loaded      bool               `bson:"loaded" json:"loaded"`
}
