// Command audio-worker runs one approved audio capability over Worker Protocol v1.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/tileforge/octoconv-worker-audio/internal/convert"
)

func main() {
	if err := refuseLegacyStores(); err != nil {
		log.Fatalf("audio protocol: %v", err)
	}
	if path := os.Getenv("AUDIO_MODEL_PATH"); path != "" {
		convert.SetAudioModelPath(path)
	}
	if raw := os.Getenv("AUDIO_THREADS"); raw != "" {
		n, err := strconv.Atoi(firstField(raw))
		if err != nil || n < 1 {
			log.Fatalf("audio protocol: AUDIO_THREADS must be a positive integer")
		}
		convert.SetAudioThreads(n)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	dial, err := loadProtocolDial()
	if err != nil {
		log.Fatalf("audio protocol: %v", err)
	}
	log.Printf("audio protocol worker starting")
	if err := runProtocol(ctx, dial); err != nil {
		log.Fatalf("audio protocol: %v", err)
	}
}

func refuseLegacyStores() error {
	if os.Getenv("DATABASE_URL") != "" || os.Getenv("REDIS_ADDR") != "" {
		return errors.New("DATABASE_URL and REDIS_ADDR are not used")
	}
	return nil
}

func audioFreeSlots() (int, error) {
	raw := os.Getenv("AUDIO_WORKER_CONCURRENCY")
	if raw == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(firstField(raw))
	if err != nil || n != 1 {
		return 0, errors.New("AUDIO_WORKER_CONCURRENCY must be 1")
	}
	return 1, nil
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(firstField(v)); err == nil {
			return d
		}
	}
	return def
}

func firstField(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			return s[:i]
		}
	}
	return s
}
