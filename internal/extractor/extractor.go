package extractor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ProgressCallback receives updates emitted by ffmpeg execution.
type ProgressCallback func(percent int, status, message string)

// Service wraps ffmpeg/ffprobe operations.
type Service struct {
	logger *slog.Logger
}

func NewService(logger *slog.Logger) *Service {
	return &Service{logger: logger}
}

// ExtractAudio runs ffmpeg and reports progress using callback.
func (s *Service) ExtractAudio(ctx context.Context, inputPath, outputPath, format, quality string, cb ProgressCallback) error {
	duration, err := s.probeDuration(ctx, inputPath)
	if err != nil {
		s.logger.Warn("could not probe duration, progress will be coarse", "error", err)
	}

	args := []string{"-y", "-i", inputPath, "-vn"}
	args = append(args, codecAndQualityArgs(format, quality)...)
	args = append(args,
		"-progress", "pipe:1",
		"-nostats",
		outputPath,
	)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create ffmpeg stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create ffmpeg stderr pipe: %w", err)
	}

	if cb != nil {
		cb(0, "processing", "iniciando extração")
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	stderrScanner := bufio.NewScanner(stderr)
	var lastErrLine string
	go func() {
		for stderrScanner.Scan() {
			line := strings.TrimSpace(stderrScanner.Text())
			if line != "" {
				lastErrLine = line
			}
		}
	}()

	scanner := bufio.NewScanner(stdout)
	progress := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "out_time_ms=") {
			msStr := strings.TrimPrefix(line, "out_time_ms=")
			if duration > 0 {
				if outMs, convErr := strconv.ParseFloat(msStr, 64); convErr == nil {
					currentSeconds := outMs / 1_000_000.0
					ratio := currentSeconds / duration
					if ratio < 0 {
						ratio = 0
					}
					if ratio > 1 {
						ratio = 1
					}
					progress = int(ratio * 100)
					if cb != nil {
						cb(progress, "processing", "extraindo áudio")
					}
				}
			}
		}
		if strings.HasPrefix(line, "progress=end") {
			progress = 100
			if cb != nil {
				cb(progress, "processing", "finalizando arquivo")
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed while reading ffmpeg output: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}
		if lastErrLine != "" {
			return fmt.Errorf("ffmpeg failed: %s", lastErrLine)
		}
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	if cb != nil {
		cb(100, "completed", "extração concluída")
	}
	return nil
}

func (s *Service) probeDuration(ctx context.Context, inputPath string) (float64, error) {
	cmd := exec.CommandContext(ctx,
		"ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe error: %w", err)
	}
	val := strings.TrimSpace(string(out))
	if val == "" {
		return 0, errors.New("empty duration response")
	}
	dur, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration from ffprobe: %w", err)
	}
	return dur, nil
}

func codecAndQualityArgs(format, quality string) []string {
	format = strings.ToLower(strings.TrimSpace(format))
	quality = strings.ToLower(strings.TrimSpace(quality))

	if format == "" {
		format = "mp3"
	}

	args := []string{"-f", format}

	switch format {
	case "mp3":
		args = append(args, "-codec:a", "libmp3lame")
		switch quality {
		case "low":
			args = append(args, "-b:a", "96k")
		case "high":
			args = append(args, "-b:a", "320k")
		case "original":
			args = append(args, "-q:a", "0")
		default:
			args = append(args, "-b:a", "192k")
		}
	case "wav":
		args = append(args, "-codec:a", "pcm_s16le")
	case "aac":
		args = append(args, "-codec:a", "aac")
		switch quality {
		case "low":
			args = append(args, "-b:a", "96k")
		case "high":
			args = append(args, "-b:a", "320k")
		case "original":
			args = append(args, "-b:a", "384k")
		default:
			args = append(args, "-b:a", "192k")
		}
	case "flac":
		args = append(args, "-codec:a", "flac")
		if quality == "high" || quality == "original" {
			args = append(args, "-compression_level", "12")
		} else {
			args = append(args, "-compression_level", "8")
		}
	case "ogg":
		args = append(args, "-codec:a", "libvorbis")
		switch quality {
		case "low":
			args = append(args, "-qscale:a", "2")
		case "high", "original":
			args = append(args, "-qscale:a", "8")
		default:
			args = append(args, "-qscale:a", "5")
		}
	default:
		args = []string{"-codec:a", "copy"}
	}

	return args
}

func OutputName(jobID, format string) string {
	format = strings.TrimPrefix(strings.ToLower(format), ".")
	if format == "" {
		format = "mp3"
	}
	return fmt.Sprintf("%s.%s", filepath.Base(jobID), format)
}
