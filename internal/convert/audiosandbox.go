package convert

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Measured read-only set for ffmpeg and whisper-cli. The model file is
// granted separately because AUDIO_MODEL_PATH is configurable.
var audioReadOnly = []string{
	"/lib",
	"/usr/lib",
	"/usr/local/lib",
	"/etc/ld.so.cache",
	"/proc/cpuinfo",
	"/sys/devices/system/cpu/online",
	"/sys/devices/system/cpu/possible",
}

// ffmpeg and whisper-cli are the two stages this process runs. ffprobe is
// on the measured engine list; this process does not call it.
var audioEngines = []string{"ffmpeg", "ffprobe", "whisper-cli"}

const audioPreflightOutsideBase = "/var/tmp"

func (c AudioConverter) WithSandbox(helper []string) AudioConverter {
	c.sandbox = newEngineSandbox(helper)
	return c
}

func (c AudioConverter) readOnlyGrants() []string {
	ro := append([]string{}, audioReadOnly...)
	model := c.model()
	ro = append(ro, model)
	if resolved, err := filepath.EvalSymlinks(model); err == nil && resolved != model {
		ro = append(ro, resolved)
	}
	return ro
}

func (c AudioConverter) engine(workDir string) engineExec {
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	return c.sandbox.forJobRW(workDir, nil, c.readOnlyGrants(), audioEngines).withEnv(engineEnv(""))
}

// AudioProof is what AudioPreflight observed inside the job directory.
type AudioProof struct {
	NormBytes       int
	TranscriptBytes int
}

func AudioPreflight(ctx context.Context, c AudioConverter) (AudioProof, error) {
	if !c.sandbox.confines() {
		return AudioProof{}, nil
	}
	w, cleanup, err := newPreflightWorkspaceOutside("audio", audioPreflightOutsideBase)
	if err != nil {
		return AudioProof{}, err
	}
	defer cleanup()
	grants := append(c.readOnlyGrants(), audioEnginePaths()...)
	if err := w.requireOutsideGrants(grants); err != nil {
		return AudioProof{}, err
	}

	inside, outside, err := w.place("in.wav", audioPreflightWAV())
	if err != nil {
		return AudioProof{}, err
	}
	outInside := filepath.Join(w.jobDir, "out.json")
	if err := c.Convert(ctx, inside, outInside, nil); err != nil {
		return AudioProof{}, fmt.Errorf("the engine cannot convert under the sandbox: %w", err)
	}
	normPath := filepath.Join(w.jobDir, "norm.wav")
	norm, err := os.ReadFile(normPath)
	if err != nil {
		return AudioProof{}, fmt.Errorf("the engine cannot convert under the sandbox: read normalized output: %w", err)
	}
	if len(norm) <= 44 || !bytes.HasPrefix(norm, []byte("RIFF")) || !bytes.Contains(norm[:44], []byte("WAVE")) {
		return AudioProof{}, fmt.Errorf("the engine cannot convert under the sandbox: normalized output is not a RIFF/WAVE file")
	}
	raw, err := os.ReadFile(outInside)
	if err != nil {
		return AudioProof{}, fmt.Errorf("the engine cannot convert under the sandbox: read transcription output: %w", err)
	}
	var transcript struct {
		Transcription json.RawMessage `json:"transcription"`
	}
	if err := json.Unmarshal(raw, &transcript); err != nil {
		return AudioProof{}, fmt.Errorf("the engine cannot convert under the sandbox: whisper-cli output is not valid JSON: %w", err)
	}
	if transcript.Transcription == nil {
		return AudioProof{}, fmt.Errorf("the engine cannot convert under the sandbox: whisper-cli JSON output has no transcription array")
	}

	if err := os.Remove(normPath); err != nil {
		return AudioProof{}, fmt.Errorf("remove normalized output before the negative run: %w", err)
	}
	outOutside := filepath.Join(w.jobDir, "neg.json")
	convErr := c.Convert(ctx, outside, outOutside, nil)
	if err := deniedOutside(convErr, outOutside, normPath); err != nil {
		return AudioProof{}, fmt.Errorf("the engine read a file outside the job directory: %w", err)
	}
	return AudioProof{NormBytes: len(norm), TranscriptBytes: len(raw)}, nil
}

func deniedOutside(convErr error, paths ...string) error {
	if convErr == nil {
		return fmt.Errorf("engine accepted the outside file")
	}
	if errors.Is(convErr, ErrEngineSandboxUnavailable) {
		return fmt.Errorf("sandbox setup failed: %w", convErr)
	}
	if strings.Contains(convErr.Error(), "locate engine ") {
		return fmt.Errorf("engine binary missing: %w", convErr)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("engine wrote %s", path)
		}
	}
	return nil
}

func audioEnginePaths() []string {
	var paths []string
	for _, name := range audioEngines {
		if p, err := exec.LookPath(name); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}

func audioPreflightWAV() []byte {
	const (
		sampleRate    = 16000
		channels      = 1
		bitsPerSample = 16
		seconds       = 1
	)
	numSamples := sampleRate * seconds
	data := make([]byte, numSamples*channels*bitsPerSample/8)
	for i := 0; i < numSamples; i++ {
		v := math.Sin(2 * math.Pi * 440 * float64(i) / float64(sampleRate))
		sample := int16(v * 32767 * 0.5)
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+len(data)))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(byteRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(bitsPerSample))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
	buf.Write(data)
	return buf.Bytes()
}
