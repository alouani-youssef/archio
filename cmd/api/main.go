package main

import (
	"archio/internal/app"
	"archio/internal/config"
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

func run() (runErr error) {
	cfg, err := config.Load(config.LoadOptions{RequireDotEnv: false})
	if err != nil {
		return fmt.Errorf("load application configuration: %w", err)
	}

	startupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	application, err := app.New(startupCtx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, application.Close())
	}()

	log.Printf("starting server on %s:%d (tls=%v)", cfg.App.Host, cfg.App.Port, cfg.TLS.Enabled)
	if cfg.App.IsDev {
		log.Printf("development metrics available at http://%s:%d/debug/statsviz/", cfg.App.Host, cfg.App.Port)
	}
	return application.Run(context.Background())
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("application stopped: %v", err)
	}
}
