package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bongocat-server/internal/config"
	appserver "bongocat-server/internal/server"
)

func main() {
	settings, err := config.Parse(os.Args[1:])
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	application := appserver.New(settings)
	httpServer := appserver.HTTPServer(settings, application.Handler())

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("BongoCat room server listening", "address", settings.Listen, "maxRooms", settings.MaxRooms, "maxPlayers", settings.MaxPlayersPerRoom, "streamMode", settings.StreamMode)
		serverErrors <- httpServer.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-stop:
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		slog.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}
}
