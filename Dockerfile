# Etapa de compilación: hace falta gcc porque el driver de SQLite
# (mattn/go-sqlite3) usa cgo, no es Go puro.
FROM golang:1.27-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags "-X main.version=${VERSION}" -o /out/nostr-relay-khatru .

# Etapa final: imagen mínima, solo el binario y los certificados del sistema.
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=build /out/nostr-relay-khatru ./nostr-relay-khatru

ENV RELAY_DB_PATH=/app/data/relay.sqlite
ENV RELAY_LISTEN_ADDR=:3334

EXPOSE 3334
VOLUME ["/app/data"]

ENTRYPOINT ["./nostr-relay-khatru"]
