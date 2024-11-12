package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/yolocs/ar-terraform-registry/internal/version"

	"github.com/abcxyz/pkg/logging"
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
		"version", version.Version)

	return nil
}
