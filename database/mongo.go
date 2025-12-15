package database

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var Client *mongo.Client
var UserCollection *mongo.Collection
var StockCollection *mongo.Collection
var CatalogCollection *mongo.Collection
var SellsCollection *mongo.Collection

func Connect(uri string, dbName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}

	Client = client
	db := client.Database(dbName)
	UserCollection = db.Collection("users")
	StockCollection = db.Collection("stock")
	CatalogCollection = db.Collection("catalog")
	SellsCollection = db.Collection("sells")
}

func GetCollection(name string) *mongo.Collection {
	return Client.Database("VerduStock").Collection(name)
}
