// Package convert turns one audio or video file into a transcript with ffmpeg and whisper-cli.
package convert

import (
	"context"
	"strings"
	"time"
)

const (
	EngineAudio          = "audio"
	OperationConvert     = "convert"
	DefaultEngineTimeout = 742 * time.Second
	MaxFileBytes         = 100 << 20
)

// Pair is one source format and one target format.
type Pair struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Converter converts the file at inPath into outPath.
type Converter interface {
	Pairs() []Pair
	Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error
}

// MIMEType is the content type the core stores for format.
func MIMEType(format string) string {
	switch NormalizeFormat(format) {
	case "txt":
		return "text/plain"
	case "srt":
		return "application/x-subrip"
	case "vtt":
		return "text/vtt"
	case "json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

// NormalizeFormat lowercases a format or extension and strips one leading dot.
func NormalizeFormat(f string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(f), "."))
}

type opKey struct {
	Operation string
	From      string
	To        string
}

// Registry maps an operation and a format pair to a converter.
type Registry struct {
	m map[opKey]Converter
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{m: make(map[opKey]Converter)}
}

// Register indexes each pair c reports.
func (r *Registry) Register(c Converter) {
	for _, pair := range c.Pairs() {
		r.m[opKey{
			Operation: OperationConvert,
			From:      NormalizeFormat(pair.From),
			To:        NormalizeFormat(pair.To),
		}] = c
	}
}

// LookupOp reports the converter for op, from, and to.
func (r *Registry) LookupOp(op, from, to string) (Converter, bool) {
	c, ok := r.m[opKey{
		Operation: op,
		From:      NormalizeFormat(from),
		To:        NormalizeFormat(to),
	}]
	return c, ok
}

// Default is the process registry. init registers AudioConverter.
var Default = NewRegistry()
