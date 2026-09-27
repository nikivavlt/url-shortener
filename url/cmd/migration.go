package main

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func initDB(ctx context.Context, db *mongo.Database, collectionName string) error {
	validator := bson.M{
		"$jsonSchema": bson.M{
			"bsonType": "object",
			"required": bson.A{
				"_id",
				"short_code",
				"original_url",
				"user_id",
				"created_at",
				"updated_at",
				"is_active",
			},
			"properties": bson.M{
				"_id":          bson.M{"bsonType": "objectId"},
				"short_code":   bson.M{"bsonType": "string", "pattern": "^[0-9A-Za-z]{7}$"},
				"original_url": bson.M{"bsonType": "string", "maxLength": 2048},
				"user_id":      bson.M{"bsonType": "string"},
				"created_at":   bson.M{"bsonType": "date"},
				"updated_at":   bson.M{"bsonType": "date"},
				"expires_at":   bson.M{"bsonType": "date"},
				"is_active":    bson.M{"bsonType": "bool"},
			},
		},
	}

	err := db.CreateCollection(ctx, collectionName,
		options.CreateCollection().
			SetValidator(validator).
			SetValidationLevel("strict").
			SetValidationAction("error"),
	)
	if err != nil {
		var cmdErr mongo.CommandError
		if !errors.As(err, &cmdErr) || cmdErr.Code != 48 {
			return err
		}
	}

	_, err = db.Collection(collectionName).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "short_code", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "_id", Value: 1}},
		},
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
	})

	return err
}
