package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"bongocat-server/internal/config"
	"github.com/coder/websocket"
)

type Server struct {
	config config.Config
	hub    *Hub
	mux    *http.ServeMux
}

func New(config config.Config) *Server {
	server := &Server{config: config, hub: NewHub(config), mux: http.NewServeMux()}
	server.mux.HandleFunc("GET /healthz", server.health)
	server.mux.HandleFunc("GET /v1/ws", server.websocket)
	return server
}

func (server *Server) Handler() http.Handler {
	return server.mux
}

func (server *Server) Hub() *Hub {
	return server.hub
}

func (server *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"status": "ok",
		"rooms":  server.hub.RoomCount(),
	})
}

func (server *Server) websocket(writer http.ResponseWriter, request *http.Request) {
	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	connection.SetReadLimit(server.config.MaxMessageBytes)

	context, cancel := context.WithCancel(request.Context())
	defer cancel()
	NewClient(server.hub, connection).Run(context)
}

func HTTPServer(config config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              config.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
