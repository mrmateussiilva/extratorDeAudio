FROM golang:1.22-alpine AS builder
WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /bin/audio-extractor ./cmd/web

FROM alpine:latest
RUN apk add --no-cache ffmpeg ca-certificates tzdata
WORKDIR /app

COPY --from=builder /bin/audio-extractor /app/audio-extractor
COPY --from=builder /app/static /app/static
COPY --from=builder /app/templates /app/templates

RUN mkdir -p /app/uploads /app/outputs

EXPOSE 8080

ENV APP_ADDR=:8080
ENV UPLOADS_DIR=/app/uploads
ENV OUTPUTS_DIR=/app/outputs
ENV MAX_UPLOAD_BYTES=524288000

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

CMD ["/app/audio-extractor"]
