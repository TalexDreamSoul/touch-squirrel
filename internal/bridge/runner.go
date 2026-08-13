package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/grok-free-register/grok-reg/internal/artifact"
	"github.com/grok-free-register/grok-reg/internal/jobs"
)

// Config bundles the runtime parameters for one bridge execution.
type Config struct {
	PluginID    string
	BridgePath  string // absolute path to runner.py or equivalent
	PythonExe   string // venv/bin/python or system python3
	Target      int
	PluginCfg   map[string]any
	Env         map[string]string
	OutputDir   string
	ArtifactDir string // root for artifact store
	LogPath     string // optional: tee job logs to this run log file
}

// RunnerResult is the terminal outcome.
type RunnerResult struct {
	JobID string
	OK    int
	Fail  int
	Total int
	Error error
}

// Run starts a bridge subprocess and blocks until completion.
// It manages a jobs.Job for the caller to stream SSE progress.
//
// The caller is responsible for:
//   - Registering the returned job with a jobs.Manager
//   - Subscribing SSE listeners to the job before calling Run
//   - Calling job.Cancel() to terminate early (SIGTERM → os.Kill)
func Run(ctx context.Context, mgr *jobs.Manager, cfg Config) (RunnerResult, error) {
	// Validate prerequisites.
	if cfg.BridgePath == "" {
		return RunnerResult{}, fmt.Errorf("plugin %s: missing bridge entry", cfg.PluginID)
	}
	if cfg.PythonExe == "" {
		cfg.PythonExe = findPython()
	}
	if cfg.PythonExe == "" {
		return RunnerResult{}, fmt.Errorf("bridge: no python interpreter found (set GROK_PYTHON or ensure python3 is in PATH)")
	}

	// Validate bridge script exists.
	if _, err := os.Stat(cfg.BridgePath); err != nil {
		return RunnerResult{}, fmt.Errorf("bridge: script not found at %s: %w", cfg.BridgePath, err)
	}

	// Prepare output directory.
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return RunnerResult{}, fmt.Errorf("bridge: mkdir output dir: %w", err)
	}

	// Build job input JSON.
	input := map[string]any{
		"jobId":     "",
		"target":    cfg.Target,
		"config":    cfg.PluginCfg,
		"outputDir": cfg.OutputDir,
	}
	if cfg.Env != nil {
		input["env"] = cfg.Env
	}
	stdinPayload, err := json.Marshal(input)
	if err != nil {
		return RunnerResult{}, fmt.Errorf("bridge: marshal input: %w", err)
	}

	// Create job for tracking.
	items := make([]*jobs.Item, cfg.Target)
	for i := range items {
		items[i] = &jobs.Item{Status: jobs.ItemPending}
	}
	job := jobs.NewJob(cfg.PluginID, items)
	input["jobId"] = job.ID
	stdinPayload, _ = json.Marshal(input)

	if mgr != nil {
		mgr.Add(job)
	}

	// Optional: tee job logs to a run log file so failures are visible in the panel.
	var logFile *os.File
	if cfg.LogPath != "" {
		if f, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			logFile = f
			defer f.Close()
		}
	}
	logf := func(format string, args ...any) {
		job.AddLog(format, args...)
		if logFile != nil {
			_, _ = fmt.Fprintf(logFile, "%s %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
		}
	}

	defer func() {
		if job.Done() {
			return
		}
		job.SetStatus(jobs.StatusFailed)
	}()

	job.SetStatus(jobs.StatusRunning)
	if b, err := json.Marshal(input); err == nil {
		logf("bridge: config %s", string(b))
	}

	// Set up subprocess.
	cmdCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, cfg.PythonExe, cfg.BridgePath)
	cmd.Dir = filepath.Dir(cfg.BridgePath)

	// Environment values must be unique. Repeated keys rely on platform-specific
	// precedence and can silently select the wrong registrar checkout.
	cmd.Env = mergedEnvironment(os.Environ(), cfg.Env, map[string]string{
		"PYTHONUNBUFFERED": "1",
	})

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return RunnerResult{}, fmt.Errorf("bridge: stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return RunnerResult{}, fmt.Errorf("bridge: stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return RunnerResult{}, fmt.Errorf("bridge: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		job.SetStatus(jobs.StatusFailed)
		logf("bridge: start failed: %v", err)
		return RunnerResult{JobID: job.ID, Error: err}, err
	}
	logf("bridge: started pid=%d plugin=%s", cmd.Process.Pid, cfg.PluginID)

	// Feed stdin.
	go func() {
		defer stdinPipe.Close()
		if _, err := io.WriteString(stdinPipe, string(stdinPayload)+"\n"); err != nil {
			logf("bridge: stdin write error: %v", err)
		}
	}()

	// Consume stderr to job logs.
	go func() {
		sc := bufio.NewScanner(stderrPipe)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				logf("bridge: stderr: %s", line)
			}
		}
	}()

	// Open artifact store for this plugin.
	artStore := artifact.NewStore(cfg.ArtifactDir)

	result := RunnerResult{JobID: job.ID, Total: cfg.Target}

	// Read stdout NDJSON.
	sc := bufio.NewScanner(stdoutPipe)
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024) // generous buffer
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		ev, err := Parse([]byte(line))
		if err != nil {
			logf("bridge: parse: %v (line=%s)", err, truncate(line, 120))
			continue
		}

		switch ev.Type {
		case EventProgress:
			var p Progress
			if err := json.Unmarshal(ev.Raw, &p); err == nil {
				logf("[%d/%d] %s", p.Done, p.Total, p.Email)
				if p.Done > 0 && p.Done <= len(items) {
					job.MutateItem(p.Done-1, func(it *jobs.Item) { it.Status = jobs.ItemSuccess })
				}
			} else {
				logf("bridge: bad progress: %s", line)
			}

		case EventLog:
			var l Log
			if err := json.Unmarshal(ev.Raw, &l); err == nil {
				logf("%s", l.Msg)
			}

		case EventCaptcha:
			var c Captcha
			if err := json.Unmarshal(ev.Raw, &c); err == nil {
				logf("[captcha] %s %s", c.Status, c.Platform)
			}

		case EventArtifact:
			var a Artifact
			if err := json.Unmarshal(ev.Raw, &a); err == nil {
				logf("[artifact] %s %s", a.Kind, a.File)
				// Try to ingest the artifact.
				if err := ingestArtifact(artStore, cfg.PluginID, a, cfg.OutputDir); err != nil {
					logf("bridge: artifact ingest error: %v", err)
				}
			}

		case EventDone:
			var d Done
			if err := json.Unmarshal(ev.Raw, &d); err == nil {
				result.OK = d.OK
				result.Fail = d.Fail
			}
			logf("[done] ok=%d fail=%d total=%d", result.OK, result.Fail, result.Total)

		case EventError:
			var e Error
			if err := json.Unmarshal(ev.Raw, &e); err == nil {
				logf("[error] attempt=%d %s %s", e.Attempt, e.Email, e.Msg)
			}

		default:
			logf("bridge: unknown event: %s", line)
		}

		// Check cancellation.
		select {
		case <-ctx.Done():
			logf("bridge: context cancelled, killing subprocess")
			cancel()
			// Graceful then force.
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
				time.Sleep(2 * time.Second)
				_ = cmd.Process.Kill()
			}
			job.SetStatus(jobs.StatusCancelled)
			result.Error = ctx.Err()
			return result, nil
		default:
		}
	}

	if err := sc.Err(); err != nil {
		logf("bridge: stdout scanner error: %v", err)
	}

	// Wait for process to exit.
	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		if exiterr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exiterr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	logf("bridge: process exited code=%d ok=%d fail=%d", exitCode, result.OK, result.Fail)

	// Determine final job status.
	if result.Error != nil {
		job.SetStatus(jobs.StatusFailed)
	} else if result.Fail == result.Total && result.Total > 0 {
		job.SetStatus(jobs.StatusFailed)
	} else {
		job.SetStatus(jobs.StatusCompleted)
	}

	return result, nil
}

// ingestArtifact reads an artifact file from the output dir and stores it.
func ingestArtifact(store *artifact.Store, pluginID string, a Artifact, outputDir string) error {
	artifactPath := a.File
	if !filepath.IsAbs(artifactPath) {
		artifactPath = filepath.Join(outputDir, artifactPath)
	}

	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return fmt.Errorf("read artifact file: %w", err)
	}

	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("artifact is not valid JSON: %w", err)
	}

	labels := map[string]string{}
	if a.Email != "" {
		labels["email"] = a.Email
	}
	if a.Username != "" {
		labels["username"] = a.Username
	}

	_, err = store.PutJSON(pluginID, a.Kind, artifact.StatusFresh, labels, payload, "")
	return err
}

// findPython looks for a Python interpreter.
// Precedence: GROK_PYTHON env → python3 on PATH → python on PATH.
func mergedEnvironment(base []string, overrides ...map[string]string) []string {
	values := make(map[string]string, len(base))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			values[key] = value
		}
	}
	for _, set := range overrides {
		for key, value := range set {
			if key != "" {
				values[key] = value
			}
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}

func findPython() string {
	if s := os.Getenv("GROK_PYTHON"); s != "" {
		return s
	}
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
