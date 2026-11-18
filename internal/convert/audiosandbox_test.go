package convert

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tileforge/octoconv-worker-audio/internal/sandbox"
)

func TestAudioGrantsMatchTheMeasuredSet(t *testing.T) {
	if strings.Join(audioReadOnly, "\n") != strings.Join([]string{
		"/lib",
		"/usr/lib",
		"/usr/local/lib",
		"/etc/ld.so.cache",
		"/proc/cpuinfo",
		"/sys/devices/system/cpu/online",
		"/sys/devices/system/cpu/possible",
	}, "\n") {
		t.Fatalf("read only %v", audioReadOnly)
	}
	if strings.Join(audioEngines, "\n") != "ffmpeg\nffprobe\nwhisper-cli" {
		t.Fatalf("engines %v", audioEngines)
	}
	if audioPreflightOutsideBase == "/tmp" {
		t.Fatal("outside fixture is under a path the ruleset must not grant")
	}
}

func TestWithSandboxEmptyHelperIsUnconfined(t *testing.T) {
	c := AudioConverter{}.WithSandbox(nil)
	if c.sandbox.confines() {
		t.Fatal("confined")
	}
	proof, err := AudioPreflight(t.Context(), c)
	if err != nil || proof.NormBytes != 0 || proof.TranscriptBytes != 0 {
		t.Fatalf("proof %+v err %v", proof, err)
	}
}

func TestForJobGrantsTheModelFileAndEngines(t *testing.T) {
	dir := t.TempDir()
	for _, name := range audioEngines {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	model := filepath.Join(dir, "ggml-base.bin")
	if err := os.WriteFile(model, []byte("model"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := AudioConverter{modelPath: model}.WithSandbox([]string{"/usr/local/bin/audio-worker", sandbox.ExecMode})
	spec := strings.Join(c.engine(filepath.Join(dir, "job")).spec, "\n")
	for _, want := range []string{
		"rw:" + filepath.Join(dir, "job"),
		"ro:" + model,
		"ro:/lib",
		"ro:/proc/cpuinfo",
		"ro:" + filepath.Join(dir, "ffmpeg"),
		"ro:" + filepath.Join(dir, "ffprobe"),
		"ro:" + filepath.Join(dir, "whisper-cli"),
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("missing %s in %s", want, spec)
		}
	}
	if strings.Contains(spec, "rw:/tmp") || strings.Contains(spec, "rw:/models") {
		t.Fatalf("spec grants a shared writable path or the model directory: %s", spec)
	}
	var bare AudioConverter
	if got := bare.engine("/job").spec; len(got) != 0 {
		t.Fatalf("unconfined spec %q", got)
	}
}

func TestDeniedOutsideRejectsSetupFailureAndAWrittenFile(t *testing.T) {
	if err := deniedOutside(ErrEngineSandboxUnavailable); err == nil || !strings.Contains(err.Error(), "sandbox setup failed") {
		t.Fatal(err)
	}
	if err := deniedOutside(errors.New("locate engine ffmpeg: exec: not found")); err == nil || !strings.Contains(err.Error(), "engine binary missing") {
		t.Fatal(err)
	}
	dir := t.TempDir()
	written := filepath.Join(dir, "neg.json")
	if err := os.WriteFile(written, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := deniedOutside(errors.New("ffmpeg failed: Permission denied"), written); err == nil || !strings.Contains(err.Error(), "engine wrote") {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "absent.json")
	if err := deniedOutside(errors.New("ffmpeg failed: error: input file not found"), missing); err != nil {
		t.Fatal(err)
	}
	if err := deniedOutside(nil, missing); err == nil {
		t.Fatal("nil error accepted")
	}
}
