package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"archio/internal/config"
	"archio/internal/database"
	"archio/internal/modules/health"
	"archio/internal/server"
	"github.com/arl/statsviz"
)

// App owns the infrastructure and transport lifecycles for one API process.
type App struct {
	connection *database.Connection
	handler    http.Handler
	settings   server.Settings
}

// New opens PostgreSQL, applies the Ent schema, and wires feature handlers.
func New(ctx context.Context, cfg config.Config) (*App, error) {
	connection, err := database.Open(ctx, cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	if err := connection.Migrate(ctx); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("initialize database schema: %w", err)
	}

	mux := server.NewMux(health.NewHandler(connection))
	if cfg.App.IsDev {
		statsviz.Register(mux)
	}

	return &App{
		connection: connection,
		handler:    mux,
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

// Run serves HTTP until the context is canceled or the process receives a stop signal.
func (a *App) Run(ctx context.Context) error {
	return server.Run(ctx, a.handler, a.settings)
}

// Close releases application resources.
func (a *App) Close() error {
	if a == nil || a.connection == nil {
		return nil
	}
	return a.connection.Close()
}
