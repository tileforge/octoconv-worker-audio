package convert

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func init() {
	Default.Register(AudioConverter{})
}

// AudioConverter shells out to ffmpeg and then whisper-cli.
// A zero value runs those binaries directly. WithSandbox applies Landlock.
type AudioConverter struct {
	modelPath string
	threads   int
	run       func(ctx context.Context, name string, args ...string) error
	sandbox   *engineSandbox
}

const defaultAudioModelPath = "/models/ggml-base.bin"

const (
	minFfmpegBudget      = 30 * time.Second
	minFfmpegBudgetVideo = 90 * time.Second
)

var (
	audioModelPath string
	audioThreads   int
	audioSources   = []string{"mp3", "wav", "m4a", "ogg", "mp4", "mov", "avi", "mkv", "webm"}
	audioTargets   = []string{"txt", "srt", "vtt", "json"}
	videoSources   = map[string]bool{"mp4": true, "mov": true, "avi": true, "mkv": true, "webm": true}
)

// SetAudioModelPath stores the model path used when a converter has none of its own.
func SetAudioModelPath(path string) {
	audioModelPath = path
}

// SetAudioThreads stores the whisper-cli thread count used when a converter has none of its own.
func SetAudioThreads(n int) {
	audioThreads = n
}

func audioThreadCount() int {
	if audioThreads > 0 {
		return audioThreads
	}
	return runtime.NumCPU()
}

// Pairs returns every source and target combination, source then target.
func (AudioConverter) Pairs() []Pair {
	pairs := make([]Pair, 0, len(audioSources)*len(audioTargets))
	for _, from := range audioSources {
		for _, to := range audioTargets {
			pairs = append(pairs, Pair{From: from, To: to})
		}
	}
	return pairs
}

// Convert transcribes inPath into outPath. language is a closed set and
// translate is a boolean. An unknown key fails before either program runs.
func (c AudioConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	parsed, err := audioOptsFromMap(opts)
	if err != nil {
		return err
	}
	target := NormalizeFormat(filepath.Ext(outPath))
	outFlags := whisperOutputFlag(target)
	if outFlags == nil {
		return fmt.Errorf("audio: unsupported target format %q", target)
	}
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < selectMinFfmpegBudget(inPath) {
		return fmt.Errorf("audio: insufficient attempt budget remaining: %w", context.DeadlineExceeded)
	}
	workDir := filepath.Dir(outPath)
	normPath := filepath.Join(workDir, "norm.wav")
	if err := c.exec(ctx, workDir, "ffmpeg", ffmpegNormalizeArgs(inPath, normPath)...); err != nil {
		return fmt.Errorf("audio: ffmpeg: %w", err)
	}
	outBase := strings.TrimSuffix(outPath, filepath.Ext(outPath))
	args := whisperArgs(c.model(), normPath, outBase, outFlags, parsed, c.threadCount())
	if err := c.exec(ctx, workDir, "whisper-cli", args...); err != nil {
		return fmt.Errorf("audio: whisper-cli: %w", err)
	}
	return validateAudioOutput(outPath)
}

func (c AudioConverter) exec(ctx context.Context, workDir, name string, args ...string) error {
	if c.run != nil {
		return c.run(ctx, name, args...)
	}
	return c.engine(workDir).run(ctx, name, args...)
}

func (c AudioConverter) model() string {
	if c.modelPath != "" {
		return c.modelPath
	}
	if audioModelPath != "" {
		return audioModelPath
	}
	return defaultAudioModelPath
}

func (c AudioConverter) threadCount() int {
	if c.threads > 0 {
		return c.threads
	}
	return audioThreadCount()
}

func selectMinFfmpegBudget(inPath string) time.Duration {
	if videoSources[NormalizeFormat(filepath.Ext(inPath))] {
		return minFfmpegBudgetVideo
	}
	return minFfmpegBudget
}

// whisper-cli writes the transcript beside -of and appends its own extension.
// -oj is segments only. -ojf is the flag that also emits token timestamps.
func whisperOutputFlag(target string) []string {
	switch target {
	case "txt":
		return []string{"-otxt"}
	case "srt":
		return []string{"-osrt"}
	case "vtt":
		return []string{"-ovtt"}
	case "json":
		return []string{"-ojf"}
	default:
		return nil
	}
}

// ffmpeg treats a bare -i operand as a URL. The file: prefix and the protocol
// whitelist keep a crafted name from selecting another protocol. -map 0:a:0
// selects the first audio stream. Without -map, ffmpeg's own pick is not stable.
func ffmpegNormalizeArgs(inPath, normPath string) []string {
	return []string{
		"-y", "-nostdin", "-protocol_whitelist", "file,crypto",
		"-i", "file:" + inPath,
		"-map", "0:a:0",
		"-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", "file:" + normPath,
	}
}

type audioOpts struct {
	language  string
	translate bool
}

var audioLanguages = map[string]bool{
	"auto": true,
	"en":   true,
	"ru":   true,
	"es":   true,
	"fr":   true,
	"de":   true,
}

func audioOptsFromMap(m map[string]any) (audioOpts, error) {
	var o audioOpts
	for k, v := range m {
		switch k {
		case "language":
			s, ok := v.(string)
			if !ok || (s != "" && !audioLanguages[s]) {
				return audioOpts{}, fmt.Errorf("audio: unsupported option")
			}
			o.language = s
		case "translate":
			b, ok := v.(bool)
			if !ok {
				return audioOpts{}, fmt.Errorf("audio: unsupported option")
			}
			o.translate = b
		default:
			return audioOpts{}, fmt.Errorf("audio: unsupported option")
		}
	}
	return o, nil
}

// whisper-cli defaults to English and to the host CPU count. An empty
// language request has to pass -l auto, and -t has to be explicit.
func whisperArgs(model, normPath, outBase string, outFlags []string, o audioOpts, threads int) []string {
	args := []string{"-m", model, "-f", normPath, "-of", outBase}
	args = append(args, outFlags...)
	lang := o.language
	if lang == "" {
		lang = "auto"
	}
	args = append(args, "-l", lang)
	if o.translate {
		args = append(args, "-tr")
	}
	args = append(args, "-t", strconv.Itoa(threads))
	return args
}

func validateAudioOutput(path string) error {
	fi, err := statOutput(path)
	if err != nil {
		return fmt.Errorf("audio: stat output: %w", err)
	}
	if fi.Size() == 0 {
		return fmt.Errorf("audio: output is empty")
	}
	return nil
}
