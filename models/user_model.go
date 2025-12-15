package models

import "go.mongodb.org/mongo-driver/bson/primitive"

type User struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Email    string             `bson:"email" json:"email"`
	Password string             `bson:"password" json:"password"`
	Username string             `bson:"username" json:"username"`
	Theme    string             `bson:"theme,omitempty" json:"theme,omitempty"`
	Language string             `bson:"language,omitempty" json:"language,omitempty"`
}
