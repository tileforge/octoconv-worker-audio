package convert

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPairsMatchManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "schema", "manifest-audio-convert.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Capabilities []struct {
			ID     string `json:"id"`
			Legacy *struct {
				Operation string `json:"operation"`
				Engine    string `json:"engine"`
			} `json:"legacy"`
			Formats []Pair `json:"formats"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Capabilities) != 1 || doc.Capabilities[0].ID != "octoconv.convert.audio" {
		t.Fatalf("capability %+v", doc.Capabilities)
	}
	legacy := doc.Capabilities[0].Legacy
	if legacy == nil || legacy.Engine != EngineAudio || legacy.Operation != OperationConvert {
		t.Fatalf("legacy %+v", legacy)
	}
	want := (AudioConverter{}).Pairs()
	got := doc.Capabilities[0].Formats
	if len(got) != 36 || len(got) != len(want) {
		t.Fatalf("formats %d", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pair %d = %+v, want %+v", i, got[i], want[i])
		}
		conv, ok := Default.LookupOp(OperationConvert, got[i].From, got[i].To)
		if !ok {
			t.Fatalf("missing %s to %s", got[i].From, got[i].To)
		}
		if _, isAudio := conv.(AudioConverter); !isAudio {
			t.Fatalf("%s to %s converter %T", got[i].From, got[i].To, conv)
		}
	}
}

func TestFfmpegNormalizeArgs(t *testing.T) {
	got := ffmpegNormalizeArgs("/work/in.mp3", "/work/norm.wav")
	want := []string{
		"-y", "-nostdin", "-protocol_whitelist", "file,crypto",
		"-i", "file:/work/in.mp3",
		"-map", "0:a:0",
		"-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", "file:/work/norm.wav",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("args = %v", got)
	}
}

func TestWhisperArgsUseAutoAndNoTranslate(t *testing.T) {
	got := whisperArgs("/models/ggml-base.bin", "/work/norm.wav", "/work/out", []string{"-ojf"}, 2)
	want := []string{"-m", "/models/ggml-base.bin", "-f", "/work/norm.wav", "-of", "/work/out", "-ojf", "-l", "auto", "-t", "2"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("args = %v", got)
	}
	for _, flag := range got {
		if flag == "-tr" {
			t.Fatal("translate flag")
		}
	}
}

func TestConvertRejectsOptionsBeforeExec(t *testing.T) {
	for _, opts := range []map[string]any{
		{"language": "ru"},
		{"translate": true},
		{"language": "en", "translate": false},
	} {
		called := false
		c := AudioConverter{run: func(context.Context, string, ...string) error {
			called = true
			return nil
		}}
		err := c.Convert(context.Background(), "/work/in.mp3", filepath.Join(t.TempDir(), "out.txt"), opts)
		if err == nil || !strings.Contains(err.Error(), "unsupported option") || called {
			t.Fatalf("opts %v err %v called %v", opts, err, called)
		}
	}
}

func TestConvertRejectsUnknownTargetBeforeExec(t *testing.T) {
	called := false
	c := AudioConverter{run: func(context.Context, string, ...string) error {
		called = true
		return nil
	}}
	err := c.Convert(context.Background(), "/missing.wav", filepath.Join(t.TempDir(), "out.xyz"), nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported target format") || called {
		t.Fatalf("err %v called %v", err, called)
	}
}

func TestConvertStopsWhenTheBudgetIsShort(t *testing.T) {
	for _, in := range []string{"/work/in.mp3", "/work/in.mkv"} {
		called := false
		c := AudioConverter{run: func(context.Context, string, ...string) error {
			called = true
			return nil
		}}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := c.Convert(ctx, in, filepath.Join(t.TempDir(), "out.txt"), nil)
		cancel()
		if err == nil || !errors.Is(err, context.DeadlineExceeded) || called {
			t.Fatalf("%s err %v called %v", in, err, called)
		}
		if strings.Contains(err.Error(), "audio: ffmpeg:") {
			t.Fatalf("%s err %v", in, err)
		}
	}
}

func TestConvertRunsFfmpegThenWhisper(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.srt")
	inPath := filepath.Join(dir, "in.wav")
	var names []string
	c := AudioConverter{
		modelPath: "/models/ggml-base.bin",
		threads:   1,
		run: func(_ context.Context, name string, args ...string) error {
			names = append(names, name)
			if name == "ffmpeg" && args[5] != "file:"+inPath {
				t.Errorf("ffmpeg input %q", args[5])
			}
			if name == "whisper-cli" {
				if err := os.WriteFile(outPath, []byte("caption"), 0o600); err != nil {
					return err
				}
			}
			return nil
		},
	}
	if err := os.WriteFile(inPath, []byte("wav"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Convert(context.Background(), inPath, outPath, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "ffmpeg,whisper-cli" {
		t.Fatalf("commands %v", names)
	}
}

func TestConvertRejectsAnEmptyWhisperFile(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	c := AudioConverter{run: func(_ context.Context, name string, _ ...string) error {
		if name == "whisper-cli" {
			return os.WriteFile(outPath, nil, 0o600)
		}
		return nil
	}}
	err := c.Convert(context.Background(), filepath.Join(dir, "in.mp3"), outPath, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "output is empty") {
		t.Fatal(err)
	}
}

func TestSelectMinFfmpegBudget(t *testing.T) {
	for _, ext := range []string{"mp4", "mov", "avi", "mkv", "webm"} {
		if selectMinFfmpegBudget("/work/in."+ext) != minFfmpegBudgetVideo {
			t.Fatalf("%s floor", ext)
		}
	}
	for _, ext := range []string{"mp3", "wav", "m4a", "ogg"} {
		if selectMinFfmpegBudget("/work/in."+ext) != minFfmpegBudget {
			t.Fatalf("%s floor", ext)
		}
	}
}

func TestMIMETypes(t *testing.T) {
	if MIMEType("txt") != "text/plain" || MIMEType("srt") != "application/x-subrip" || MIMEType("vtt") != "text/vtt" || MIMEType("json") != "application/json" {
		t.Fatalf("mime txt %s srt %s vtt %s json %s", MIMEType("txt"), MIMEType("srt"), MIMEType("vtt"), MIMEType("json"))
	}
}
