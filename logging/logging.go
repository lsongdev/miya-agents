package logging

import (
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Config struct {
	Enabled bool   `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Level   string `json:"level,omitempty" yaml:"level,omitempty"`
	Stdout  bool   `json:"stdout,omitempty" yaml:"stdout,omitempty"`
	File    string `json:"file,omitempty" yaml:"file,omitempty"`
}

var mu sync.Mutex

func SetupFromDefaultConfig(appName string) error {
	cfg := Config{Enabled: true, Level: "info"}
	if loaded, ok := loadConfig(); ok {
		cfg = loaded
		if strings.TrimSpace(cfg.Level) == "" {
			cfg.Level = "info"
		}
	}
	return Setup(appName, cfg)
}

func Setup(appName string, cfg Config) error {
	mu.Lock()
	defer mu.Unlock()

	if strings.TrimSpace(cfg.Level) == "" {
		cfg.Level = "info"
	}
	if !cfg.Enabled {
		w := levelWriter{level: parseLevel(cfg.Level), out: io.Discard}
		log.SetOutput(w)
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: parseSlogLevel(cfg.Level)})))
		return nil
	}

	path := strings.TrimSpace(cfg.File)
	if path == "" {
		dir, err := logsDir()
		if err != nil {
			return err
		}
		path = filepath.Join(dir, safeName(appName)+".log")
	} else {
		path = expandPath(path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	var out io.Writer = file
	if cfg.Stdout {
		out = io.MultiWriter(os.Stderr, file)
	}
	w := levelWriter{level: parseLevel(cfg.Level), out: out}
	log.SetOutput(w)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	slog.SetDefault(slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: parseSlogLevel(cfg.Level)})))
	log.Printf("[INFO] logging initialized app=%s file=%s level=%s", appName, path, cfg.Level)
	return nil
}

func loadConfig() (Config, bool) {
	path, err := configPath()
	if err != nil {
		return Config{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return Config{}, false
	}
	var raw struct {
		Logging *Config `json:"logging"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, false
	}
	if raw.Logging != nil {
		return *raw.Logging, true
	}
	return Config{}, false
}

type level int

const (
	levelDebug level = iota
	levelInfo
	levelWarn
	levelError
)

type levelWriter struct {
	level level
	out   io.Writer
}

func (w levelWriter) Write(p []byte) (int, error) {
	if messageLevel(string(p)) < w.level {
		return len(p), nil
	}
	if w.out == nil {
		return len(p), nil
	}
	if _, err := w.out.Write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func messageLevel(s string) level {
	upper := strings.ToUpper(s)
	switch {
	case strings.Contains(upper, "[DEBUG]"), strings.Contains(upper, " DEBUG "):
		return levelDebug
	case strings.Contains(upper, "[WARN]"), strings.Contains(upper, " WARN "):
		return levelWarn
	case strings.Contains(upper, "[ERROR]"), strings.Contains(upper, " ERROR "):
		return levelError
	default:
		return levelInfo
	}
}

func parseLevel(raw string) level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return levelDebug
	case "warn", "warning":
		return levelWarn
	case "error":
		return levelError
	default:
		return levelInfo
	}
}

func parseSlogLevel(raw string) slog.Level {
	switch parseLevel(raw) {
	case levelDebug:
		return slog.LevelDebug
	case levelWarn:
		return slog.LevelWarn
	case levelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func logsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".miya", "logs"), nil
	}
	return filepath.Join(home, ".miya", "logs"), nil
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".miya", "config.json"), nil
	}
	return filepath.Join(home, ".miya", "config.json"), nil
}

func expandPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func safeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "miya"
	}
	return b.String()
}
