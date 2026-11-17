package events

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	"archio/internal/config"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Broker owns the RabbitMQ connection lifecycle.
type Broker struct {
	connection *amqp.Connection

	closeOnce sync.Once
	closeErr  error
}

// Open connects to RabbitMQ using the configured credentials and verifies a channel.
func Open(ctx context.Context, cfg config.BrokerConfig) (*Broker, error) {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	connection, err := amqp.DialConfig(rabbitMQURI(cfg), amqp.Config{
		Heartbeat: 10 * time.Second,
		Dial: func(network, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("connect to rabbitmq: %w", err)
	}

	broker := &Broker{connection: connection}
	if err := broker.Ping(ctx); err != nil {
		_ = broker.Close()
		return nil, fmt.Errorf("verify rabbitmq connection: %w", err)
	}
	return broker, nil
}

// Connection returns the AMQP connection for event modules.
func (b *Broker) Connection() *amqp.Connection {
	return b.connection
}

// Ping verifies the broker is open and can create an AMQP channel.
func (b *Broker) Ping(ctx context.Context) error {
	if b == nil || b.connection == nil || b.connection.IsClosed() {
		return errors.New("rabbitmq connection is not open")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	channel, err := b.connection.Channel()
	if err != nil {
		return err
	}
	return channel.Close()
}

// Close releases the RabbitMQ connection once.
func (b *Broker) Close() error {
	if b == nil || b.connection == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		if !b.connection.IsClosed() {
			b.closeErr = b.connection.Close()
		}
	})
	return b.closeErr
}

func rabbitMQURI(cfg config.BrokerConfig) string {
	uri := url.URL{
		Scheme: "amqp",
		User:   url.UserPassword(cfg.Username, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   "//", // RabbitMQ's default virtual host is "/".
	}
	return uri.String()
}
