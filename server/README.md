# BongoCat Multiplayer Server

This directory contains the stateless Go room relay. It stores rooms only in memory and does not persist input, names, or skin assets.

```bash
go run ./cmd/bongocat-server
```

The default listener is `:8080`; health is available at `GET /healthz` and WebSocket clients connect to `GET /v1/ws`. For a constrained stream:

```bash
go run ./cmd/bongocat-server --stream-mode=limited --continuous-hz=20
```

Production deployments should place the service behind a TLS reverse proxy and expose the WebSocket endpoint over WSS. Run `go test -race ./...` before deployment.
