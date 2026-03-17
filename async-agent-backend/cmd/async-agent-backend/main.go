package main

import (
	"log/slog"
	"os"

	"async-agent-backend/internal/app"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	application := app.New(logger)
	if err := application.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		logger.Error("command failed", "error", err.Error())
		os.Exit(1)
	}
}
