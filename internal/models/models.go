package models

import "time"

// JobStatus represents the current state of an extraction job.
type JobStatus string

const (
	StatusUploaded   JobStatus = "uploaded"
	StatusQueued     JobStatus = "queued"
	StatusProcessing JobStatus = "processing"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)

// ExtractionJob stores metadata and runtime state for a conversion request.
type ExtractionJob struct {
	ID            string    `json:"id"`
	InputFileName string    `json:"input_file_name"`
	InputPath     string    `json:"input_path"`
	OutputPath    string    `json:"output_path"`
	OutputName    string    `json:"output_name"`
	Format        string    `json:"format"`
	Quality       string    `json:"quality"`
	Status        JobStatus `json:"status"`
	Progress      int       `json:"progress"`
	Error         string    `json:"error"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ProgressEvent is sent to clients over WebSocket.
type ProgressEvent struct {
	ID          string    `json:"id"`
	Status      JobStatus `json:"status"`
	Progress    int       `json:"progress"`
	Message     string    `json:"message,omitempty"`
	DownloadURL string    `json:"download_url,omitempty"`
	Error       string    `json:"error,omitempty"`
}
