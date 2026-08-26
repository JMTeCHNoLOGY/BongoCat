FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bongocat-server ./cmd/bongocat-server

FROM scratch
COPY --from=build /bongocat-server /bongocat-server
EXPOSE 8080
ENTRYPOINT ["/bongocat-server"]
