FROM golang:1.22-alpine AS builder
WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /bin/audio-extractor ./cmd/web

FROM alpine:latest AS whisper-builder
RUN apk add --no-cache git cmake make g++ bash curl

RUN git clone --depth=1 https://github.com/ggerganov/whisper.cpp.git /tmp/whisper.cpp
RUN cmake -S /tmp/whisper.cpp -B /tmp/whisper.cpp/build -DWHISPER_BUILD_EXAMPLES=ON -DWHISPER_BUILD_TESTS=OFF
RUN cmake --build /tmp/whisper.cpp/build --config Release -j$(nproc)
RUN /tmp/whisper.cpp/models/download-ggml-model.sh base
RUN if [ -f /tmp/whisper.cpp/build/bin/whisper-cli ]; then cp /tmp/whisper.cpp/build/bin/whisper-cli /tmp/whisper-cli; else cp /tmp/whisper.cpp/build/bin/main /tmp/whisper-cli; fi

FROM alpine:latest
RUN apk add --no-cache ffmpeg ca-certificates tzdata libstdc++ wget
WORKDIR /app

COPY --from=builder /bin/audio-extractor /app/audio-extractor
COPY --from=builder /app/static /app/static
COPY --from=builder /app/templates /app/templates
COPY --from=whisper-builder /tmp/whisper-cli /app/whisper/whisper-cli
COPY --from=whisper-builder /tmp/whisper.cpp/models/ggml-base.bin /app/whisper/models/ggml-base.bin

RUN chmod +x /app/whisper/whisper-cli && mkdir -p /app/uploads /app/outputs

EXPOSE 8080

ENV APP_ADDR=:8080
ENV UPLOADS_DIR=/app/uploads
ENV OUTPUTS_DIR=/app/outputs
ENV MAX_UPLOAD_BYTES=524288000
ENV WHISPER_BIN=/app/whisper/whisper-cli
ENV WHISPER_MODEL=/app/whisper/models/ggml-base.bin
ENV WHISPER_LANGUAGE=auto

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

CMD ["/app/audio-extractor"]
