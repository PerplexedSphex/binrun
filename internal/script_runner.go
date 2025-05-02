package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"log/slog"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// ScriptRunner manages script creation and execution via NATS commands.
type ScriptRunner struct {
	nc         *nats.Conn
	js         jetstream.JetStream
	scriptsDir string
	langs      map[string]LangImpl
	jobs       sync.Map // jobID -> cancelFunc
}

// LangImpl defines language-specific script init/run logic.
type LangImpl interface {
	Init(ctx context.Context, dir string) error
	Run(ctx context.Context, dir string, args []string, env map[string]string) *exec.Cmd
}

// jobState tracks a running script job.
type jobState struct {
	cancel context.CancelFunc
}

// NewScriptRunner constructs a ScriptRunner with language adapters.
func NewScriptRunner(nc *nats.Conn, js jetstream.JetStream, scriptsDir string) *ScriptRunner {
	langs := map[string]LangImpl{
		"python":     pythonImpl{},
		"typescript": tsImpl{},
	}
	return &ScriptRunner{
		nc:         nc,
		js:         js,
		scriptsDir: scriptsDir,
		langs:      langs,
	}
}

// Start subscribes to script create/run commands and manages job lifecycle.
func (sr *ScriptRunner) Start(ctx context.Context) error {
	// Create consumers for script.create and script.*.run
	if err := sr.setupConsumer(ctx, "SCRIPT_CREATE", "command.script.create", sr.handleCreate); err != nil {
		return err
	}

	if err := sr.setupConsumer(ctx, "SCRIPT_RUN", "command.script.*.run", sr.handleRun); err != nil {
		return err
	}

	// Shutdown: cancel all jobs on context done
	go func() {
		<-ctx.Done()
		sr.stopAllJobs()
	}()

	return nil
}

// setupConsumer creates/updates a consumer and starts consumption
func (sr *ScriptRunner) setupConsumer(ctx context.Context, name string, subject string, handler func(context.Context, jetstream.Msg)) error {
	_, err := sr.js.CreateOrUpdateConsumer(ctx, "COMMAND", jetstream.ConsumerConfig{
		Durable:        name,
		AckPolicy:      jetstream.AckExplicitPolicy,
		FilterSubjects: []string{subject},
	})
	if err != nil {
		return fmt.Errorf("create %s consumer: %w", name, err)
	}

	consumer, err := sr.js.Consumer(ctx, "COMMAND", name)
	if err != nil {
		return fmt.Errorf("get %s consumer: %w", name, err)
	}

	_, err = consumer.Consume(func(msg jetstream.Msg) {
		handler(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("consume %s: %w", name, err)
	}

	return nil
}

// handleCreate processes command.script.create messages.
func (sr *ScriptRunner) handleCreate(ctx context.Context, msg jetstream.Msg) {
	type createPayload struct {
		ScriptName    string `json:"script_name"`
		ScriptType    string `json:"script_type"`
		CorrelationID string `json:"correlation_id,omitempty"`
	}

	slog.Info("Received script create command", "payload", string(msg.Data()))

	var in createPayload
	if err := json.Unmarshal(msg.Data(), &in); err != nil {
		slog.Error("Invalid create payload", "err", err)
		_ = msg.Ack()
		return
	}

	dir := filepath.Join(sr.scriptsDir, in.ScriptName)
	slog.Info("Creating script directory", "name", in.ScriptName, "type", in.ScriptType, "dir", dir)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("Failed to create script directory", "dir", dir, "err", err)
		sr.publishEvent(
			fmt.Sprintf("event.script.%s.create.error", in.ScriptName),
			map[string]any{"error": err.Error(), "correlation_id": in.CorrelationID},
			in.CorrelationID,
		)
		_ = msg.Ack()
		return
	}

	impl := sr.langs[in.ScriptType]
	if impl == nil {
		slog.Error("Unsupported script type", "script_type", in.ScriptType)
		sr.publishEvent(
			fmt.Sprintf("event.script.%s.create.error", in.ScriptName),
			map[string]any{"error": "unsupported script_type", "correlation_id": in.CorrelationID},
			in.CorrelationID,
		)
		_ = msg.Ack()
		return
	}

	if err := impl.Init(ctx, dir); err != nil {
		slog.Error("Script initialization failed", "name", in.ScriptName, "err", err)
		sr.publishEvent(
			fmt.Sprintf("event.script.%s.create.error", in.ScriptName),
			map[string]any{"error": err.Error(), "correlation_id": in.CorrelationID},
			in.CorrelationID,
		)
		_ = msg.Ack()
		return
	}

	slog.Info("Script created successfully", "name", in.ScriptName, "type", in.ScriptType)
	sr.publishEvent(
		fmt.Sprintf("event.script.%s.created", in.ScriptName),
		map[string]any{"correlation_id": in.CorrelationID},
		in.CorrelationID,
	)
	_ = msg.Ack()
}

// handleRun processes command.script.<name>.run messages.
func (sr *ScriptRunner) handleRun(ctx context.Context, msg jetstream.Msg) {
	type runPayload struct {
		Args          []string          `json:"args,omitempty"`
		Env           map[string]string `json:"env,omitempty"`
		CorrelationID string            `json:"correlation_id,omitempty"`
	}

	// Extract script name from subject
	parts := strings.Split(msg.Subject(), ".")
	if len(parts) < 4 {
		slog.Error("Malformed run subject", "subject", msg.Subject())
		_ = msg.Ack()
		return
	}

	scriptName := parts[2]
	var in runPayload
	if err := json.Unmarshal(msg.Data(), &in); err != nil {
		slog.Error("Invalid run payload", "err", err)
		_ = msg.Ack()
		return
	}

	dir := filepath.Join(sr.scriptsDir, scriptName)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		sr.publishEvent(
			fmt.Sprintf("event.script.%s.job.error", scriptName),
			map[string]any{"error": "script not found", "correlation_id": in.CorrelationID},
			in.CorrelationID,
		)
		_ = msg.Ack()
		return
	}

	// Find script type by looking for known files
	scriptType := sr.detectScriptType(dir)
	impl := sr.langs[scriptType]
	if impl == nil {
		sr.publishEvent(
			fmt.Sprintf("event.script.%s.job.error", scriptName),
			map[string]any{"error": "unknown script type", "correlation_id": in.CorrelationID},
			in.CorrelationID,
		)
		_ = msg.Ack()
		return
	}

	jobID := uuid.NewString()
	jobCtx, cancel := context.WithCancel(ctx)
	sr.jobs.Store(jobID, jobState{cancel: cancel})

	cmd := impl.Run(jobCtx, dir, in.Args, in.Env)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		sr.publishEvent(
			fmt.Sprintf("event.script.%s.job.%s.exit", scriptName, jobID),
			map[string]any{"exit_code": -1, "error": err.Error(), "correlation_id": in.CorrelationID},
			in.CorrelationID,
		)
		cancel()
		sr.jobs.Delete(jobID)
		_ = msg.Ack()
		return
	}

	// Publish started event
	sr.publishEvent(
		fmt.Sprintf("event.script.%s.job.%s.started", scriptName, jobID),
		map[string]any{"pid": cmd.Process.Pid, "correlation_id": in.CorrelationID},
		in.CorrelationID,
	)

	// Pump stdout/stderr
	go sr.pumpOutput(jobCtx, stdout, scriptName, jobID, "stdout", in.CorrelationID)
	go sr.pumpOutput(jobCtx, stderr, scriptName, jobID, "stderr", in.CorrelationID)

	// Wait for exit
	go sr.waitForExit(cmd, scriptName, jobID, cancel, in.CorrelationID)

	_ = msg.Ack()
}

// detectScriptType determines script type by file presence
func (sr *ScriptRunner) detectScriptType(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "main.py")); err == nil {
		return "python"
	} else if _, err := os.Stat(filepath.Join(dir, "index.ts")); err == nil {
		return "typescript"
	}
	return ""
}

// waitForExit waits for the process to exit and publishes event
func (sr *ScriptRunner) waitForExit(cmd *exec.Cmd, scriptName, jobID string, cancel context.CancelFunc, correlationID string) {
	err := cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
		slog.Error("Script process exited with error", "script", scriptName, "job_id", jobID, "code", exitCode, "err", err)
	} else {
		slog.Info("Script process completed successfully", "script", scriptName, "job_id", jobID)
	}

	sr.publishEvent(
		fmt.Sprintf("event.script.%s.job.%s.exit", scriptName, jobID),
		map[string]any{"exit_code": exitCode, "correlation_id": correlationID},
		correlationID,
	)

	cancel()
	sr.jobs.Delete(jobID)
}

// pumpOutput streams output lines as events.
func (sr *ScriptRunner) pumpOutput(ctx context.Context, r io.Reader, scriptName, jobID, stream, correlationID string) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024) // up to 1MB lines

	for scanner.Scan() {
		line := scanner.Text()
		sr.publishEvent(
			fmt.Sprintf("event.script.%s.job.%s.%s", scriptName, jobID, stream),
			map[string]any{"data": line, "correlation_id": correlationID},
			correlationID,
		)

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// publishEvent sends an event to the EVENT stream with optional correlation_id header.
func (sr *ScriptRunner) publishEvent(subject string, body map[string]any, correlationID string) {
	data, _ := json.Marshal(body)
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  nats.Header{},
	}
	if correlationID != "" {
		msg.Header.Set("correlation_id", correlationID)
	}
	_ = sr.nc.PublishMsg(msg)
}

// stopAllJobs cancels all running jobs.
func (sr *ScriptRunner) stopAllJobs() {
	sr.jobs.Range(func(key, value any) bool {
		if js, ok := value.(jobState); ok {
			js.cancel()
		}
		sr.jobs.Delete(key)
		return true
	})
}

// pythonImpl implements LangImpl for Python scripts.
type pythonImpl struct{}

func (pythonImpl) Init(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "uv", "init")
	cmd.Dir = dir
	return cmd.Run()
}

func (pythonImpl) Run(ctx context.Context, dir string, args []string, env map[string]string) *exec.Cmd {
	allArgs := append([]string{"run", "main.py"}, args...)
	cmd := exec.CommandContext(ctx, "uv", allArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), mapToEnv(env)...)
	return cmd
}

// tsImpl implements LangImpl for TypeScript scripts.
type tsImpl struct{}

func (tsImpl) Init(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "bun", "init")
	cmd.Dir = dir
	return cmd.Run()
}

func (tsImpl) Run(ctx context.Context, dir string, args []string, env map[string]string) *exec.Cmd {
	allArgs := append([]string{"run", "index.ts"}, args...)
	cmd := exec.CommandContext(ctx, "bun", allArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), mapToEnv(env)...)
	return cmd
}

// mapToEnv converts a map to []string{"k=v"} for os/exec.
func mapToEnv(m map[string]string) []string {
	if m == nil {
		return []string{}
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, fmt.Sprintf("%s=%s", k, v))
	}
	return out
}
