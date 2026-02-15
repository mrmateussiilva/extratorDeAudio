# Audio Extractor (Go + templ + ffmpeg)

Aplicação web em Go para extrair áudio de vídeos com processamento assíncrono via `ffmpeg`, interface moderna com Tailwind e atualização de progresso em tempo real por WebSocket.

## Stack

- Go 1.22
- [templ](https://github.com/a-h/templ)
- chi router
- gorilla/websocket
- ffmpeg
- Docker + Docker Compose

## Funcionalidades

- Upload de vídeo com drag & drop
- Limite de upload: **500MB**
- Formatos de saída: `mp3`, `wav`, `aac`, `flac`, `ogg`
- Qualidade: `low`, `medium`, `high`, `original`
- Processamento assíncrono
- Barra de progresso em tempo real (WebSocket)
- Download automático ao concluir
- Lista de extrações recentes
- Endpoint de health check (`/healthz`)
- Graceful shutdown
- Limpeza automática de jobs/arquivos antigos (24h)

## Estrutura do projeto

```text
audio-extractor/
├── cmd/
│   └── web/
│       └── main.go
├── internal/
│   ├── handlers/
│   │   └── handlers.go
│   ├── extractor/
│   │   └── extractor.go
│   └── models/
│       └── models.go
├── templates/
│   ├── index.templ
│   ├── upload.templ
│   ├── result.templ
│   ├── index_templ.go
│   ├── upload_templ.go
│   └── result_templ.go
├── static/
│   ├── css/
│   │   └── style.css
│   └── js/
│       └── app.js
├── uploads/
├── outputs/
├── go.mod
├── go.sum
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── .dockerignore
├── .gitignore
└── README.md
```

## Screenshots ASCII

### Tela inicial

```text
+-------------------------------------------------------------+
| Audio Extractor                        Go + templ + ffmpeg  |
+-------------------------------------------------------------+
| [ Arraste e solte o vídeo aqui ]                            |
|   ou clique para selecionar (máx. 500MB)                    |
|                                                             |
| Formato: [ MP3 v ]   Qualidade: [ Média v ]                |
|                                                             |
| [            Extrair Áudio            ]                     |
+-------------------------------------------------------------+
| Extrações recentes                                           |
| - video1.mp4 | mp3 | medium | completed      [Download]     |
| - aula.mov   | wav | high   | processing     [Abrir]        |
+-------------------------------------------------------------+
```

### Tela de processamento

```text
+-------------------------------------------------------------+
| Extração em andamento                         [Nova extração]|
+-------------------------------------------------------------+
| Arquivo: video.mp4                                            |
| Formato: MP3 | Qualidade: medium                              |
| [#########################.................] 62%              |
| extraindo áudio                                               |
|                                                              |
| (ao concluir: download automático + botão Baixar áudio)      |
+-------------------------------------------------------------+
```

## Endpoints

- `GET /` página inicial
- `POST /upload` upload do vídeo
- `GET /job/{id}` página de progresso do job
- `GET /extract/{id}` inicia extração assíncrona
- `GET /download/{id}` download do áudio pronto
- `GET /ws/{id}` progresso em tempo real via WebSocket
- `GET /healthz` health check

## Rodando local (sem Docker)

Pré-requisitos:
- Go 1.22+
- ffmpeg e ffprobe instalados no sistema

```bash
make run
# acessar http://localhost:8080
```

## Rodando com Docker

```bash
# Build
docker-compose build

# Run
docker-compose up

# Acessar
http://localhost:8080
```

## Makefile (comandos úteis)

```bash
make build        # compila binário em ./bin
make run          # executa aplicação local
make test         # roda testes
make fmt          # formata código Go
make clean        # limpa binário e arquivos temporários
make docker-build # build da imagem docker
make docker-up    # sobe stack com docker-compose
make docker-down  # derruba stack
make templ        # gera templates com templ (opcional)
```

## Configurações por ambiente

- `APP_ADDR` (default `:8080`)
- `UPLOADS_DIR` (default `uploads`)
- `OUTPUTS_DIR` (default `outputs`)
- `MAX_UPLOAD_BYTES` (default `524288000` = 500MB)

## Fluxo interno

1. Usuário envia vídeo em `POST /upload`.
2. Servidor salva arquivo em `uploads/` e cria job em memória.
3. UI redireciona para `/job/{id}` e chama `GET /extract/{id}`.
4. Backend executa `ffmpeg` assíncrono.
5. Progresso é enviado por WebSocket (`/ws/{id}`).
6. Ao concluir, frontend inicia download automático (`/download/{id}`).

## Observações de produção

- Logs estruturados em JSON com `slog`.
- Timeout global de request e graceful shutdown.
- Limpeza automática de jobs/arquivos com mais de 24h.
- CORS habilitado para integração em cenários cross-origin.
- Para persistência de histórico após reinício, use banco (ex.: Postgres/Redis) em vez de memória.
