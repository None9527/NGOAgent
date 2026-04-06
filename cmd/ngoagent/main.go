package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ngoclaw/ngoagent/internal/application"
)

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile)

	app, err := application.Build()
	if err != nil {
		slog.Error(fmt.Sprintf("Build failed: %v", err))
		os.Exit(1)
	}
	defer func() {
		if sqlDB, err := app.DB.DB(); err == nil {
			sqlDB.Close()
		}
		app.Config.StopWatching()
	}()

	// Wire subagent progress events → parent session SSE stream
	if app.SpawnTool != nil && app.Server != nil {
		app.SpawnTool.SetEventPusher(app.Server.PushEvent)
	}
	// Wire evo async events → WS push
	if app.Loop != nil && app.Server != nil {
		app.Loop.SetEventPusher(app.Server.PushEvent)
	}

	// Start config hot-reload watcher
	if err := app.Config.StartWatching(); err != nil {
		slog.Info(fmt.Sprintf("Warning: config watcher: %v", err))
	}

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info(fmt.Sprint("Shutting down..."))
		cancel()
	}()

	slog.Info(fmt.Sprint("NGOAgent starting..."))

	// Start gRPC server in background
	if app.GRPCServer != nil {
		go func() {
			if err := app.GRPCServer.Start(); err != nil {
				slog.Info(fmt.Sprintf("gRPC server: %v", err))
			}
		}()
	}

	// Start HTTP server (blocking)
	if err := app.Server.Start(ctx); err != nil {
		slog.Info(fmt.Sprintf("HTTP server: %v", err))
	}

	// Graceful shutdown
	if app.GRPCServer != nil {
		app.GRPCServer.Stop()
	}
}
