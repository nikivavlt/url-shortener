package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nikivavlt/url-shortener/url/internal/domain"
)

type linkDoc struct {
	ID          bson.ObjectID `bson:"_id"`
	ShortCode   string        `bson:"short_code"`
	OriginalURL string        `bson:"original_url"`
	UserID      string        `bson:"user_id"`
	CreatedAt   time.Time     `bson:"created_at"`
	UpdatedAt   time.Time     `bson:"updated_at"`
	ExpiresAt   *time.Time    `bson:"expires_at,omitempty"`
	IsActive    bool          `bson:"is_active"`
}

type URLRepository struct {
	coll *mongo.Collection
}

func New(coll *mongo.Collection) *URLRepository {
	return &URLRepository{coll: coll}
}

func (r *URLRepository) Insert(ctx context.Context, link *domain.Link) error {
	doc := toDoc(link)
	doc.ID = bson.NewObjectID()

	if _, err := r.coll.InsertOne(ctx, doc); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return domain.ErrShortCodeTaken
		}
		return fmt.Errorf("insert link: %w", err)
	}

	link.ID = doc.ID.Hex()
	return nil
}

func (r *URLRepository) FindByShortCode(ctx context.Context, shortCode string) (*domain.Link, error) {
	var doc linkDoc
	err := r.coll.FindOne(ctx, bson.D{{Key: "short_code", Value: shortCode}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, domain.ErrLinkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find link: %w", err)
	}
	return fromDoc(doc), nil
}

func (r *URLRepository) DeleteByOwner(ctx context.Context, shortCode, userID string) (bool, error) {
	res, err := r.coll.DeleteOne(ctx, bson.D{
		{Key: "short_code", Value: shortCode},
		{Key: "user_id", Value: userID},
	})
	if err != nil {
		return false, fmt.Errorf("delete link: %w", err)
	}
	return res.DeletedCount == 1, nil
}

func (r *URLRepository) CountActive(ctx context.Context, userID string, now time.Time) (int64, error) {
	n, err := r.coll.CountDocuments(ctx, bson.D{
		{Key: "user_id", Value: userID},
		{Key: "is_active", Value: true},
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "expires_at", Value: bson.D{{Key: "$exists", Value: false}}}},
			bson.D{{Key: "expires_at", Value: bson.D{{Key: "$gt", Value: now}}}},
		}},
	})
	if err != nil {
		return 0, fmt.Errorf("count links: %w", err)
	}
	return n, nil
}

func (r *URLRepository) ListByUser(ctx context.Context, userID, afterID string, limit int) ([]*domain.Link, error) {
	filter := bson.D{{Key: "user_id", Value: userID}}
	if afterID != "" {
		oid, err := bson.ObjectIDFromHex(afterID)
		if err != nil {
			return nil, fmt.Errorf("%w: malformed cursor", domain.ErrInvalidArgument)
		}
		filter = append(filter, bson.E{Key: "_id", Value: bson.D{{Key: "$gt", Value: oid}}})
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: 1}}).
		SetLimit(int64(limit))

	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}

	var docs []linkDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode links: %w", err)
	}

	links := make([]*domain.Link, 0, len(docs))
	for _, d := range docs {
		links = append(links, fromDoc(d))
	}
	return links, nil
}

func toDoc(l *domain.Link) linkDoc {
	return linkDoc{
		ShortCode:   l.ShortCode,
		OriginalURL: l.OriginalURL,
		UserID:      l.UserID,
		CreatedAt:   l.CreatedAt,
		UpdatedAt:   l.UpdatedAt,
		ExpiresAt:   l.ExpiresAt,
		IsActive:    l.IsActive,
	}
}

func fromDoc(d linkDoc) *domain.Link {
	return &domain.Link{
		ID:          d.ID.Hex(),
		ShortCode:   d.ShortCode,
		OriginalURL: d.OriginalURL,
		UserID:      d.UserID,
		CreatedAt:   d.CreatedAt.UTC(),
		UpdatedAt:   d.UpdatedAt.UTC(),
		ExpiresAt:   d.ExpiresAt,
		IsActive:    d.IsActive,
	}
}
