// Command audio-worker runs one approved audio capability over Worker Protocol generation 2.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tileforge/octoconv-worker-audio/internal/convert"
	"github.com/tileforge/octoconv-worker-audio/internal/sandbox"
)

const audioSandboxDisabledEnv = "AUDIO_SANDBOX_DISABLED"

func main() {
	if sandbox.RunExecModeIfRequested(os.Args) {
		return
	}
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

	sandboxHelper, err := sandbox.ResolveHelper(audioSandboxDisabledEnv)
	if err != nil {
		log.Fatalf("audio sandbox: %v", err)
	}
	conv := convert.AudioConverter{}.WithSandbox(sandboxHelper)
	convert.Default.Register(conv)
	if len(sandboxHelper) == 0 {
		log.Printf("%s=true: the engine can read every file in this container", audioSandboxDisabledEnv)
	} else {
		log.Printf("audio sandbox: landlock via %s", strings.Join(sandboxHelper, " "))
		if err := convert.StabilizeTempDir(); err != nil {
			log.Fatalf("audio sandbox: %v", err)
		}
	}

	preflightCtx, cancelPreflight := context.WithTimeout(ctx, 180*time.Second)
	proof, err := convert.AudioPreflight(preflightCtx, conv)
	cancelPreflight()
	if err != nil {
		log.Fatalf("audio sandbox preflight: %v", err)
	}
	if len(sandboxHelper) > 0 {
		log.Printf("audio sandbox proven: ffmpeg and whisper-cli ran inside the job directory and were denied a file outside every grant (normalized wav %d bytes, transcript %d bytes, no outside output)", proof.NormBytes, proof.TranscriptBytes)
	}

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
