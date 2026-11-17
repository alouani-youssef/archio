package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"archio/internal/cache"
	"archio/internal/config"
	"archio/internal/database"
	"archio/internal/events"
	"archio/internal/middlewares"
	"archio/internal/modules/admin"
	"archio/internal/modules/buckets"
	"archio/internal/modules/docs"
	"archio/internal/modules/health"
	"archio/internal/modules/objects"
	"archio/internal/server"
	"archio/internal/storage"
	"github.com/arl/statsviz"
)

// App owns the infrastructure and transport lifecycles for one API process.
type App struct {
	database  *database.Connection
	cache     *cache.Connection
	broker    *events.Broker
	publisher *events.AMQPPublisher
	handler   http.Handler
	settings  server.Settings
}

// New opens PostgreSQL, applies the Ent schema, and wires feature handlers.
func New(ctx context.Context, cfg config.Config) (*App, error) {
	connection, err := database.Open(ctx, cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	cacheConnection, err := cache.Open(ctx, cfg.Redis)
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("initialize cache: %w", err)
	}

	broker, err := events.Open(ctx, cfg.Broker)
	if err != nil {
		_ = cacheConnection.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("initialize event broker: %w", err)
	}

	if err := connection.Migrate(ctx); err != nil {
		_ = broker.Close()
		_ = cacheConnection.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("initialize database schema: %w", err)
	}

	blobs, err := storage.NewLocal(cfg.Storage.Path)
	if err != nil {
		_ = broker.Close()
		_ = cacheConnection.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("initialize object storage: %w", err)
	}

	publisher, err := events.NewPublisher(broker)
	if err != nil {
		_ = broker.Close()
		_ = cacheConnection.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("initialize event publisher: %w", err)
	}

	client := connection.Client()
	basicAuth := middlewares.BasicAuth(cfg.Auth.RootUsername, cfg.Auth.RootPassword)
	keyAuth := middlewares.APIKeyAuth(admin.NewKeyAuthenticator(client))

	mux := server.NewMux(health.NewHandler(health.Dependencies{
		Postgres: connection,
		Redis:    cacheConnection,
		RabbitMQ: broker,
	}),
		func(m *http.ServeMux) { admin.RegisterRoutes(m, client, basicAuth) },
		func(m *http.ServeMux) { buckets.RegisterRoutes(m, client, keyAuth) },
		func(m *http.ServeMux) {
			objects.RegisterRoutes(m, objects.Deps{
				Client:         client,
				Storage:        blobs,
				Publisher:      publisher,
				PublicBaseURL:  cfg.Storage.PublicBaseURL,
				MaxUploadBytes: cfg.Storage.MaxUploadBytes,
			}, keyAuth)
		},
		docs.RegisterRoutes,
	)
	if cfg.App.IsDev {
		statsviz.Register(mux)
	}

	return &App{
		database:  connection,
		cache:     cacheConnection,
		broker:    broker,
		publisher: publisher,
		handler:   mux,
		settings: server.Settings{
			Host:              cfg.App.Host,
			Port:              cfg.App.Port,
			ReadTimeout:       15 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       2 * time.Minute,
			ShutdownTimeout:   10 * time.Second,
			TLS: server.TLSSettings{
				Enabled:  cfg.TLS.Enabled,
				CertFile: cfg.TLS.CertFile,
				KeyFile:  cfg.TLS.KeyFile,
			},
		},
	}, nil
}

// Handler returns the HTTP handler serving every route.
func (a *App) Handler() http.Handler { return a.handler }

// Run serves HTTP until the context is canceled or the process receives a stop signal.
func (a *App) Run(ctx context.Context) error {
	return server.Run(ctx, a.handler, a.settings)
}

// Close releases application resources.
func (a *App) Close() error {
	if a == nil {
		return nil
	}
	return errors.Join(a.publisher.Close(), a.broker.Close(), a.cache.Close(), a.database.Close())
}
