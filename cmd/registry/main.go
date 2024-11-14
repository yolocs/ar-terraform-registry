package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	ar "cloud.google.com/go/artifactregistry/apiv1"
	"github.com/abcxyz/pkg/logging"

	"github.com/yolocs/ar-terraform-registry/internal/version"
	"github.com/yolocs/ar-terraform-registry/pkg/config"
	"github.com/yolocs/ar-terraform-registry/pkg/server"
	"github.com/yolocs/ar-terraform-registry/pkg/store"
)

func main() {
	ctx, done := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer done()

	logger := logging.NewFromEnv("")
	ctx = logging.WithLogger(ctx, logger)

	if err := realMain(ctx); err != nil {
		done()
		logger.ErrorContext(ctx, err.Error())
		os.Exit(1)
	}
	logger.InfoContext(ctx, "successful shutdown")
}

func realMain(ctx context.Context) error {
	logger := logging.FromContext(ctx)
	logger.DebugContext(ctx, "server starting",
		"name", version.Name,
		"commit", version.Commit,
		"version", version.Version,
	)

	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}

	donwloader, err := store.NewDownloader(ctx)
	if err != nil {
		return err
	}

	arClient, err := ar.NewClient(ctx)
	if err != nil {
		return err
	}

	arStore, err := store.NewArtifactRegistryGeneric(
		arClient, donwloader,
		&store.Config{ProjectID: cfg.ProjectID, Location: cfg.Location})
	if err != nil {
		return err
	}

	svr, err := server.New(
		&server.Config{Port: cfg.Port},
		arStore,
		nil,
		logger,
	)
	if err != nil {
		return err
	}

	if err := svr.Start(ctx); err != nil {
		return err
	}

	return nil
}
