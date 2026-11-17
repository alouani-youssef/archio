package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	// Exchange is the durable topic exchange all archio events are published to.
	Exchange = "archio.events"
	// ObjectVersionCreated is the routing key for new object versions.
	ObjectVersionCreated = "object.version.created"
)

// Publisher publishes domain events.
type Publisher interface {
	Publish(ctx context.Context, routingKey string, payload any) error
}

// ObjectVersionCreatedEvent is the payload for ObjectVersionCreated.
type ObjectVersionCreatedEvent struct {
	VersionID      uuid.UUID `json:"versionId"`
	ObjectKey      string    `json:"objectKey"`
	BucketID       int       `json:"bucketId"`
	BucketName     string    `json:"bucketName"`
	GroupID        uuid.UUID `json:"groupId"`
	Size           int64     `json:"size"`
	ChecksumSHA256 string    `json:"checksumSha256"`
	ContentType    string    `json:"contentType"`
	CreatedAt      time.Time `json:"createdAt"`
}

// AMQPPublisher publishes JSON events over a dedicated AMQP channel.
type AMQPPublisher struct {
	// sem is a 1-slot semaphore serializing channel use (AMQP channels are not
	// goroutine-safe) while letting waiters honor context cancellation.
	sem     chan struct{}
	channel *amqp.Channel
}

// NewPublisher opens a dedicated channel and declares the durable topic exchange.
func NewPublisher(b *Broker) (*AMQPPublisher, error) {
	if b == nil || b.Connection() == nil {
		return nil, errors.New("broker is not open")
	}
	channel, err := b.Connection().Channel()
	if err != nil {
		return nil, fmt.Errorf("open publisher channel: %w", err)
	}
	if err := channel.ExchangeDeclare(Exchange, amqp.ExchangeTopic, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		return nil, fmt.Errorf("declare exchange %q: %w", Exchange, err)
	}
	return &AMQPPublisher{sem: make(chan struct{}, 1), channel: channel}, nil
}

// Publish marshals payload as JSON and publishes it persistently.
func (p *AMQPPublisher) Publish(ctx context.Context, routingKey string, payload any) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("publish %q: %w", routingKey, err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return fmt.Errorf("publish %q: %w", routingKey, ctx.Err())
	}
	if err := p.channel.PublishWithContext(ctx, Exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Body:         body,
	}); err != nil {
		return fmt.Errorf("publish %q: %w", routingKey, err)
	}
	return nil
}

// Close closes the publisher channel.
func (p *AMQPPublisher) Close() error {
	p.sem <- struct{}{}
	defer func() { <-p.sem }()
	return p.channel.Close()
}

// NoopPublisher discards events; for tests.
type NoopPublisher struct{}

// Publish implements Publisher.
func (NoopPublisher) Publish(context.Context, string, any) error { return nil }
