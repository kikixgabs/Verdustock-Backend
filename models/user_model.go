package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type User struct {
	ID    primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Email string             `bson:"email" json:"email"`
	// RECOMENDACIÓN: json:"-" para no devolver el hash de la contraseña al frontend
	Password string `bson:"password" json:"-"`
	Username string `bson:"username" json:"username"`
	Theme    string `bson:"theme,omitempty" json:"theme,omitempty"`
	Language string `bson:"language,omitempty" json:"language,omitempty"`

	// Usamos un puntero (*MPAccount) para que si no tiene cuenta, sea nil en la DB
	MPAccount          *MPAccount `bson:"mpAccount,omitempty" json:"mpAccount,omitempty"`
	MPAccountConnected bool       `bson:"mpAccountConnected" json:"mpAccountConnected"`
}

type MPAccount struct {
	// DATOS SENSIBLES (Ocultos del JSON)
	AccessToken  string `bson:"accessToken" json:"-"`  // ¡Seguridad! No enviar al front
	RefreshToken string `bson:"refreshToken" json:"-"` // ¡Seguridad! No enviar al front

	// DATOS PÚBLICOS (Útiles para el front)
	PublicKey string `bson:"publicKey" json:"publicKey,omitempty"`
	UserID    int64  `bson:"userId" json:"userId"` // El ID numérico de MP

	// CONTROL DE VIGENCIA (Para saber cuándo refrescar)
	ExpiresIn int       `bson:"expiresIn" json:"-"` // Segundos que dura el token
	UpdatedAt time.Time `bson:"updatedAt" json:"-"` // Cuándo se guardó/refrescó por última vez
}
