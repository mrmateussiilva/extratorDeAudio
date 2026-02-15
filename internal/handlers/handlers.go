package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"extratorDeAudio/internal/extractor"
	"extratorDeAudio/internal/models"
	"extratorDeAudio/templates"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
)

const (
	defaultMaxUploadBytes = 500 * 1024 * 1024
)

type App struct {
	logger *slog.Logger

	router    *chi.Mux
	extractor *extractor.Service

	uploadsDir string
	outputsDir string

	maxUploadBytes int64

	mu   sync.RWMutex
	jobs map[string]*models.ExtractionJob
	subs map[string]map[*websocket.Conn]struct{}

	upgrader websocket.Upgrader
}

func NewApp(logger *slog.Logger, uploadsDir, outputsDir string, maxUploadBytes int64, whisperBin, whisperModel, whisperLanguage string) *App {
	if maxUploadBytes <= 0 {
		maxUploadBytes = defaultMaxUploadBytes
	}

	app := &App{
		logger:         logger,
		router:         chi.NewRouter(),
		extractor:      extractor.NewService(logger, whisperBin, whisperModel, whisperLanguage),
		uploadsDir:     uploadsDir,
		outputsDir:     outputsDir,
		maxUploadBytes: maxUploadBytes,
		jobs:           make(map[string]*models.ExtractionJob),
		subs:           make(map[string]map[*websocket.Conn]struct{}),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	app.registerRoutes()
	return app
}

func (a *App) Router() http.Handler {
	return a.router
}

func (a *App) registerRoutes() {
	a.router.Use(middleware.RequestID)
	a.router.Use(middleware.RealIP)
	a.router.Use(middleware.Recoverer)
	a.router.Use(middleware.Timeout(45 * time.Minute))
	a.router.Use(a.corsMiddleware)

	a.router.Get("/", a.index)
	a.router.Post("/upload", a.upload)
	a.router.Get("/job/{id}", a.jobPage)
	a.router.Get("/extract/{id}", a.startExtraction)
	a.router.Get("/transcribe/{id}", a.startTranscription)
	a.router.Get("/download/{id}", a.download)
	a.router.Get("/transcript/{id}", a.downloadTranscript)
	a.router.Get("/ws/{id}", a.jobWS)
	a.router.Get("/healthz", a.health)

	staticFS := http.FileServer(http.Dir("static"))
	a.router.Handle("/static/*", http.StripPrefix("/static/", staticFS))
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "timestamp": time.Now().Format(time.RFC3339)})
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, templates.IndexPage(a.recentJobs(10)))
}

func (a *App) jobPage(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	job, ok := a.getJob(jobID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	a.render(w, r, templates.UploadPage(job, a.recentJobs(10)))
}

func (a *App) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, a.maxUploadBytes+1024)
	if err := r.ParseMultipartForm(a.maxUploadBytes); err != nil {
		a.logger.Warn("invalid multipart upload", "error", err)
		http.Error(w, "upload inválido ou maior que 500MB", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		http.Error(w, "arquivo de vídeo é obrigatório", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if header.Size > a.maxUploadBytes {
		http.Error(w, "arquivo excede o limite de 500MB", http.StatusBadRequest)
		return
	}

	format := sanitizeFormat(r.FormValue("format"))
	quality := sanitizeQuality(r.FormValue("quality"))

	if err := os.MkdirAll(a.uploadsDir, 0o755); err != nil {
		a.logger.Error("failed to ensure uploads dir", "error", err)
		http.Error(w, "erro interno ao preparar upload", http.StatusInternalServerError)
		return
	}

	jobID := newID()
	safeName := sanitizeFileName(header.Filename)
	inputPath := filepath.Join(a.uploadsDir, jobID+"_"+safeName)

	out, err := os.Create(inputPath)
	if err != nil {
		a.logger.Error("failed to create upload file", "error", err)
		http.Error(w, "erro ao salvar upload", http.StatusInternalServerError)
		return
	}
	defer out.Close()

	if _, err := out.ReadFrom(file); err != nil {
		a.logger.Error("failed to persist upload", "error", err)
		http.Error(w, "erro ao gravar arquivo", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	job := &models.ExtractionJob{
		ID:                 jobID,
		InputFileName:      safeName,
		InputPath:          inputPath,
		Format:             format,
		Quality:            quality,
		Status:             models.StatusUploaded,
		Progress:           0,
		TranscriptStatus:   models.StatusNotStarted,
		TranscriptProgress: 0,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	a.mu.Lock()
	a.jobs[jobID] = job
	a.mu.Unlock()

	a.logger.Info("upload saved", "job_id", jobID, "file", safeName, "format", format, "quality", quality)
	http.Redirect(w, r, "/job/"+jobID, http.StatusSeeOther)
}

func (a *App) startExtraction(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	a.mu.Lock()
	job, ok := a.jobs[jobID]
	if !ok {
		a.mu.Unlock()
		http.Error(w, "job não encontrado", http.StatusNotFound)
		return
	}

	switch job.Status {
	case models.StatusProcessing, models.StatusQueued:
		a.mu.Unlock()
		a.respondJSON(w, http.StatusAccepted, map[string]string{"status": "already_processing"})
		return
	case models.StatusCompleted:
		a.mu.Unlock()
		a.respondJSON(w, http.StatusOK, map[string]string{"status": "already_completed", "download_url": "/download/" + job.ID})
		return
	}

	job.Status = models.StatusQueued
	job.Progress = 1
	job.Error = ""
	job.TranscriptStatus = models.StatusNotStarted
	job.TranscriptProgress = 0
	job.TranscriptError = ""
	job.TranscriptTXTPath = ""
	job.TranscriptTXTName = ""
	job.TranscriptSRTPath = ""
	job.TranscriptSRTName = ""
	job.UpdatedAt = time.Now()
	a.mu.Unlock()

	a.broadcast(jobID, models.ProgressEvent{ID: jobID, Stage: "extraction", Status: models.StatusQueued, Progress: 1, Message: "job em fila"})
	go a.runExtraction(jobID)
	a.respondJSON(w, http.StatusAccepted, map[string]string{"status": "started", "job_id": jobID})
}

func (a *App) runExtraction(jobID string) {
	job, ok := a.getJob(jobID)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	outputName := extractor.OutputName(job.ID, job.Format)
	outputPath := filepath.Join(a.outputsDir, outputName)

	if err := os.MkdirAll(a.outputsDir, 0o755); err != nil {
		a.failJob(jobID, fmt.Errorf("failed to create outputs dir: %w", err))
		return
	}

	a.updateJob(jobID, func(j *models.ExtractionJob) {
		j.Status = models.StatusProcessing
		j.OutputName = outputName
		j.OutputPath = outputPath
		j.UpdatedAt = time.Now()
	})

	err := a.extractor.ExtractAudio(ctx, job.InputPath, outputPath, job.Format, job.Quality, func(percent int, status, message string) {
		if percent < 1 {
			percent = 1
		}
		a.updateJob(jobID, func(j *models.ExtractionJob) {
			j.Status = models.StatusProcessing
			j.Progress = percent
			j.UpdatedAt = time.Now()
		})
		a.broadcast(jobID, models.ProgressEvent{ID: jobID, Stage: "extraction", Status: models.StatusProcessing, Progress: percent, Message: message})
	})

	if err != nil {
		a.failJob(jobID, err)
		return
	}

	a.updateJob(jobID, func(j *models.ExtractionJob) {
		j.Status = models.StatusCompleted
		j.Progress = 100
		j.Error = ""
		j.UpdatedAt = time.Now()
	})
	a.broadcast(jobID, models.ProgressEvent{
		ID:          jobID,
		Stage:       "extraction",
		Status:      models.StatusCompleted,
		Progress:    100,
		Message:     "extração concluída",
		DownloadURL: "/download/" + jobID,
	})

	a.logger.Info("extraction completed", "job_id", jobID, "output", outputPath)
}

func (a *App) startTranscription(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	a.mu.Lock()
	job, ok := a.jobs[jobID]
	if !ok {
		a.mu.Unlock()
		http.Error(w, "job não encontrado", http.StatusNotFound)
		return
	}
	if job.Status != models.StatusCompleted {
		a.mu.Unlock()
		http.Error(w, "extração ainda não foi concluída", http.StatusConflict)
		return
	}

	switch job.TranscriptStatus {
	case models.StatusQueued, models.StatusProcessing:
		a.mu.Unlock()
		a.respondJSON(w, http.StatusAccepted, map[string]string{"status": "transcription_already_processing"})
		return
	case models.StatusCompleted:
		a.mu.Unlock()
		a.respondJSON(w, http.StatusOK, map[string]string{
			"status":             "transcription_already_completed",
			"transcript_txt_url": "/transcript/" + jobID + "?format=txt",
			"transcript_srt_url": "/transcript/" + jobID + "?format=srt",
		})
		return
	}

	job.TranscriptStatus = models.StatusQueued
	job.TranscriptProgress = 1
	job.TranscriptError = ""
	job.UpdatedAt = time.Now()
	a.mu.Unlock()

	a.broadcast(jobID, models.ProgressEvent{ID: jobID, Stage: "transcription", Status: models.StatusQueued, Progress: 1, Message: "transcrição em fila"})
	go a.runTranscription(jobID)

	a.respondJSON(w, http.StatusAccepted, map[string]string{"status": "transcription_started", "job_id": jobID})
}

func (a *App) runTranscription(jobID string) {
	job, ok := a.getJob(jobID)
	if !ok {
		return
	}
	if job.OutputPath == "" {
		a.failTranscription(jobID, fmt.Errorf("arquivo de áudio não encontrado para transcrição"))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()

	base := filepath.Join(a.outputsDir, job.ID+"_transcript")
	txtPath := base + ".txt"
	srtPath := base + ".srt"

	a.updateJob(jobID, func(j *models.ExtractionJob) {
		j.TranscriptStatus = models.StatusProcessing
		j.TranscriptProgress = 1
		j.TranscriptError = ""
		j.TranscriptTXTPath = txtPath
		j.TranscriptTXTName = filepath.Base(txtPath)
		j.TranscriptSRTPath = srtPath
		j.TranscriptSRTName = filepath.Base(srtPath)
		j.UpdatedAt = time.Now()
	})

	err := a.extractor.TranscribeAudio(ctx, job.OutputPath, base, func(percent int, status, message string) {
		if percent < 1 {
			percent = 1
		}
		a.updateJob(jobID, func(j *models.ExtractionJob) {
			j.TranscriptStatus = models.StatusProcessing
			j.TranscriptProgress = percent
			j.UpdatedAt = time.Now()
		})
		a.broadcast(jobID, models.ProgressEvent{ID: jobID, Stage: "transcription", Status: models.StatusProcessing, Progress: percent, Message: message})
	})

	if err != nil {
		a.failTranscription(jobID, err)
		return
	}

	if _, err := os.Stat(txtPath); err != nil {
		a.failTranscription(jobID, fmt.Errorf("transcrição TXT não foi gerada"))
		return
	}
	if _, err := os.Stat(srtPath); err != nil {
		a.failTranscription(jobID, fmt.Errorf("transcrição SRT não foi gerada"))
		return
	}

	a.updateJob(jobID, func(j *models.ExtractionJob) {
		j.TranscriptStatus = models.StatusCompleted
		j.TranscriptProgress = 100
		j.TranscriptError = ""
		j.UpdatedAt = time.Now()
	})

	a.broadcast(jobID, models.ProgressEvent{
		ID:               jobID,
		Stage:            "transcription",
		Status:           models.StatusCompleted,
		Progress:         100,
		Message:          "transcrição concluída",
		TranscriptTXTURL: "/transcript/" + jobID + "?format=txt",
		TranscriptSRTURL: "/transcript/" + jobID + "?format=srt",
	})
	a.logger.Info("transcription completed", "job_id", jobID)
}

func (a *App) failJob(jobID string, err error) {
	a.logger.Error("extraction failed", "job_id", jobID, "error", err)
	a.updateJob(jobID, func(j *models.ExtractionJob) {
		j.Status = models.StatusFailed
		j.Error = err.Error()
		j.UpdatedAt = time.Now()
	})
	a.broadcast(jobID, models.ProgressEvent{ID: jobID, Stage: "extraction", Status: models.StatusFailed, Progress: 0, Error: err.Error(), Message: "falha na extração"})
}

func (a *App) failTranscription(jobID string, err error) {
	a.logger.Error("transcription failed", "job_id", jobID, "error", err)
	a.updateJob(jobID, func(j *models.ExtractionJob) {
		j.TranscriptStatus = models.StatusFailed
		j.TranscriptError = err.Error()
		j.TranscriptProgress = 0
		j.UpdatedAt = time.Now()
	})
	a.broadcast(jobID, models.ProgressEvent{ID: jobID, Stage: "transcription", Status: models.StatusFailed, Progress: 0, Error: err.Error(), Message: "falha na transcrição"})
}

func (a *App) download(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	job, ok := a.getJob(jobID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if job.Status != models.StatusCompleted || job.OutputPath == "" {
		http.Error(w, "arquivo ainda não está pronto", http.StatusConflict)
		return
	}
	if _, err := os.Stat(job.OutputPath); err != nil {
		http.Error(w, "arquivo não encontrado", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+job.OutputName+"\"")
	http.ServeFile(w, r, job.OutputPath)
}

func (a *App) downloadTranscript(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	job, ok := a.getJob(jobID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if job.TranscriptStatus != models.StatusCompleted {
		http.Error(w, "transcrição ainda não está pronta", http.StatusConflict)
		return
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	path := job.TranscriptTXTPath
	name := job.TranscriptTXTName
	if format == "srt" {
		path = job.TranscriptSRTPath
		name = job.TranscriptSRTName
	}
	if path == "" {
		http.Error(w, "arquivo de transcrição não encontrado", http.StatusNotFound)
		return
	}
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "arquivo de transcrição não encontrado", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	http.ServeFile(w, r, path)
}

func (a *App) jobWS(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	job, ok := a.getJob(jobID)
	if !ok {
		http.Error(w, "job não encontrado", http.StatusNotFound)
		return
	}

	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		a.logger.Warn("websocket upgrade failed", "error", err)
		return
	}

	a.mu.Lock()
	if a.subs[jobID] == nil {
		a.subs[jobID] = make(map[*websocket.Conn]struct{})
	}
	a.subs[jobID][conn] = struct{}{}
	a.mu.Unlock()

	_ = conn.WriteJSON(currentProgressEvent(job))

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}

	a.mu.Lock()
	delete(a.subs[jobID], conn)
	a.mu.Unlock()
	_ = conn.Close()
}

func currentProgressEvent(job *models.ExtractionJob) models.ProgressEvent {
	event := models.ProgressEvent{
		ID:          job.ID,
		Stage:       "extraction",
		Status:      job.Status,
		Progress:    job.Progress,
		Error:       job.Error,
		DownloadURL: downloadURLForJob(job),
	}

	if job.TranscriptStatus == models.StatusQueued || job.TranscriptStatus == models.StatusProcessing || job.TranscriptStatus == models.StatusCompleted || job.TranscriptStatus == models.StatusFailed {
		event.Stage = "transcription"
		event.Status = job.TranscriptStatus
		event.Progress = job.TranscriptProgress
		event.Error = job.TranscriptError
		event.TranscriptTXTURL = transcriptTXTURLForJob(job)
		event.TranscriptSRTURL = transcriptSRTURLForJob(job)
	}

	return event
}

func downloadURLForJob(job *models.ExtractionJob) string {
	if job.Status == models.StatusCompleted {
		return "/download/" + job.ID
	}
	return ""
}

func transcriptTXTURLForJob(job *models.ExtractionJob) string {
	if job.TranscriptStatus == models.StatusCompleted {
		return "/transcript/" + job.ID + "?format=txt"
	}
	return ""
}

func transcriptSRTURLForJob(job *models.ExtractionJob) string {
	if job.TranscriptStatus == models.StatusCompleted {
		return "/transcript/" + job.ID + "?format=srt"
	}
	return ""
}

func (a *App) broadcast(jobID string, evt models.ProgressEvent) {
	a.mu.RLock()
	conns := make([]*websocket.Conn, 0, len(a.subs[jobID]))
	for c := range a.subs[jobID] {
		conns = append(conns, c)
	}
	a.mu.RUnlock()

	for _, c := range conns {
		if err := c.WriteJSON(evt); err != nil {
			a.mu.Lock()
			delete(a.subs[jobID], c)
			a.mu.Unlock()
			_ = c.Close()
		}
	}
}

func (a *App) render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
		a.logger.Error("failed to render template", "error", err)
		http.Error(w, "erro ao renderizar página", http.StatusInternalServerError)
	}
}

func (a *App) respondJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		a.logger.Error("failed to encode json", "error", err)
	}
}

func (a *App) getJob(id string) (*models.ExtractionJob, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	job, ok := a.jobs[id]
	if !ok {
		return nil, false
	}
	clone := *job
	return &clone, true
}

func (a *App) updateJob(id string, fn func(*models.ExtractionJob)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if job, ok := a.jobs[id]; ok {
		fn(job)
	}
}

func (a *App) recentJobs(limit int) []*models.ExtractionJob {
	a.mu.RLock()
	jobs := make([]*models.ExtractionJob, 0, len(a.jobs))
	for _, j := range a.jobs {
		clone := *j
		jobs = append(jobs, &clone)
	}
	a.mu.RUnlock()

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].UpdatedAt.After(jobs[j].UpdatedAt)
	})

	if limit > 0 && len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs
}

func (a *App) StartCleanupLoop(ctx context.Context, interval, ttl time.Duration) {
	if interval <= 0 || ttl <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.cleanup(ttl)
			}
		}
	}()
}

func (a *App) cleanup(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl)
	var oldJobs []models.ExtractionJob

	a.mu.Lock()
	for id, job := range a.jobs {
		if job.UpdatedAt.Before(cutoff) {
			oldJobs = append(oldJobs, *job)
			delete(a.jobs, id)
		}
	}
	a.mu.Unlock()

	for _, job := range oldJobs {
		if job.InputPath != "" {
			_ = os.Remove(job.InputPath)
		}
		if job.OutputPath != "" {
			_ = os.Remove(job.OutputPath)
		}
		if job.TranscriptTXTPath != "" {
			_ = os.Remove(job.TranscriptTXTPath)
		}
		if job.TranscriptSRTPath != "" {
			_ = os.Remove(job.TranscriptSRTPath)
		}
	}

	if len(oldJobs) > 0 {
		a.logger.Info("cleanup completed", "removed_jobs", len(oldJobs))
	}
}

func sanitizeFormat(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "mp3", "wav", "aac", "flac", "ogg":
		return strings.ToLower(v)
	default:
		return "mp3"
	}
}

func sanitizeQuality(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "low", "medium", "high", "original":
		return strings.ToLower(v)
	default:
		return "medium"
	}
}

func sanitizeFileName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, name)
	if name == "" {
		return "video.bin"
	}
	return name
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (a *App) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
