package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Agent struct {
		Name           string `toml:"name"`
		PollIntervalMs int    `toml:"poll_interval_ms"`
		StatePath      string `toml:"state_path"`
	} `toml:"agent"`

	Webhook struct {
		URL       string `toml:"url"`
		Token     string `toml:"token"`
		TimeoutMs int    `toml:"timeout_ms"`
	} `toml:"webhook"`

	Filter struct {
		ErrorPatterns   []string `toml:"error_patterns"`
		WarningPatterns []string `toml:"warning_patterns"`
		IgnorePatterns  []string `toml:"ignore_patterns"`
	} `toml:"filter"`

	Logs struct {
		Paths []string `toml:"paths"`
	} `toml:"logs"`
}

type State struct {
	Files map[string]FileState `json:"files"`
}

type FileState struct {
	Inode  uint64 `json:"inode"`
	Offset int64  `json:"offset"`
}

type Event struct {
	Type     string         `json:"type"`
	Message  string         `json:"message"`
	Level    string         `json:"level"`
	Source   string         `json:"source"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "Path to TOML config")
	flag.Parse()

	if strings.TrimSpace(configPath) == "" {
		fmt.Fprintln(os.Stderr, "Missing required flag: --config")
		os.Exit(2)
	}

	cfg, err := readConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Config error:", err)
		os.Exit(2)
	}

	statePath := cfg.Agent.StatePath
	if statePath == "" {
		statePath = "/var/lib/maintainai-agent/state.json"
	}

	pollMs := cfg.Agent.PollIntervalMs
	if pollMs <= 0 {
		pollMs = 500
	}

	timeoutMs := cfg.Webhook.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}

	client := &http.Client{
		Timeout: time.Duration(timeoutMs) * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	hostname, _ := os.Hostname()
	baseMeta := map[string]any{
		"agent":      "maintainai-agent",
		"agent_name": emptyToNil(strings.TrimSpace(cfg.Agent.Name)),
		"host":       hostname,
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
	}

	errRe := compileRegexes(cfg.Filter.ErrorPatterns)
	warnRe := compileRegexes(cfg.Filter.WarningPatterns)
	ignRe := compileRegexes(cfg.Filter.IgnorePatterns)

	st := loadState(statePath)
	if st.Files == nil {
		st.Files = map[string]FileState{}
	}

	paths := normalizePaths(cfg.Logs.Paths)
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "Config error: logs.paths is empty")
		os.Exit(2)
	}

	lastFlush := time.Now()
	flushEvery := 2 * time.Second

	for {
		anyUpdates := false
		for _, p := range paths {
			prev := st.Files[p]
			next, lines := readNewLines(p, prev)
			st.Files[p] = next
			if len(lines) > 0 {
				anyUpdates = true
			}

			for _, line := range lines {
				line = strings.TrimRight(line, "\r\n")
				if strings.TrimSpace(line) == "" {
					continue
				}
				if matchesAny(ignRe, line) {
					continue
				}

				level := ""
				if matchesAny(errRe, line) {
					level = "error"
				} else if matchesAny(warnRe, line) {
					level = "warning"
				} else {
					continue
				}

				meta := map[string]any{}
				for k, v := range baseMeta {
					meta[k] = v
				}
				meta["path"] = p
				meta["inode"] = next.Inode
				meta["timestamp"] = time.Now().Format(time.RFC3339)

				msg := line
				if len(msg) > 4000 {
					msg = msg[:4000]
				}

				evt := Event{
					Type:     "server_log",
					Message:  msg,
					Level:    level,
					Source:   p,
					Metadata: meta,
				}

				_ = postEvent(context.Background(), client, cfg.Webhook.URL, cfg.Webhook.Token, evt)
			}
		}

		if anyUpdates || time.Since(lastFlush) >= flushEvery {
			saveState(statePath, st)
			lastFlush = time.Now()
		}

		time.Sleep(time.Duration(pollMs) * time.Millisecond)
	}
}

func readConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := toml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	cfg.Webhook.URL = strings.TrimSpace(cfg.Webhook.URL)
	cfg.Webhook.Token = strings.TrimSpace(cfg.Webhook.Token)
	if cfg.Webhook.URL == "" || cfg.Webhook.Token == "" {
		return nil, errors.New("webhook.url and webhook.token are required")
	}
	return &cfg, nil
}

func normalizePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			continue
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func compileRegexes(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			continue
		}
		out = append(out, re)
	}
	return out
}

func matchesAny(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func loadState(path string) State {
	var st State
	b, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	return st
}

func saveState(path string, st State) {
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0o755)

	b, err := json.Marshal(st)
	if err != nil {
		return
	}

	tmp := path + ".tmp"
	_ = os.WriteFile(tmp, b, 0o644)
	_ = os.Rename(tmp, path)
}

func statInode(path string) (uint64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("stat_t unavailable")
	}
	return st.Ino, nil
}

func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func readNewLines(path string, prev FileState) (FileState, []string) {
	inode, err := statInode(path)
	if err != nil {
		return FileState{Inode: 0, Offset: 0}, nil
	}

	size, err := fileSize(path)
	if err != nil {
		return FileState{Inode: inode, Offset: prev.Offset}, nil
	}

	rotated := (prev.Inode != 0 && inode != prev.Inode) || (size < prev.Offset)
	offset := prev.Offset
	if rotated {
		offset = 0
	}

	f, err := os.Open(path)
	if err != nil {
		return FileState{Inode: inode, Offset: offset}, nil
	}
	defer f.Close()

	_, _ = f.Seek(offset, io.SeekStart)
	data, _ := io.ReadAll(f)
	newOffset, _ := f.Seek(0, io.SeekCurrent)

	if len(data) == 0 {
		return FileState{Inode: inode, Offset: offset}, nil
	}

	text := string(data)
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return FileState{Inode: inode, Offset: newOffset}, nil
	}

	last := lines[len(lines)-1]
	if last != "" && !strings.HasSuffix(text, "\n") {
		newOffset = newOffset - int64(len([]byte(last)))
		lines = lines[:len(lines)-1]
	}

	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return FileState{Inode: inode, Offset: newOffset}, lines
}

func postEvent(ctx context.Context, client *http.Client, url, token string, evt Event) error {
	b, err := json.Marshal(evt)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
}

func emptyToNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

