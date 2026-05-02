FROM golang:1.22-bookworm AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/bots-platform ./cmd/server

FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/bots-platform /app/bots-platform
COPY config /app/config
COPY registry /app/registry

ENV APP_MODE=all
ENV HTTP_ADDR=:8080

EXPOSE 8080

ENTRYPOINT ["/app/bots-platform"]

