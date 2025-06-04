// =============================================================================
// Script Runner – env layering *plus* schema‑aware I/O and code‑gen
// Copy‑paste this file into runtime/script_runner.go.  Requires:
//   • Go 1.22+
//   • github.com/joho/godotenv          (env files)
//   • github.com/santhosh-tekuri/jsonschema/v5 (JSON‑Schema validation)
//   • External CLIs for code‑gen (installed via Taskfile):
//       – npx json-schema-to-typescript
//       – datamodel-codegen
// =============================================================================

package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"binrun/internal/messages"

	"github.com/joho/godotenv"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/xid"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
)

// =============================================================================
// helpers – repo root discovery & env merging
// =============================================================================

// repoRoot walks upward from start until it finds a .git directory or go.mod file.
func repoRoot(start string) (string, error) {
	dir := filepath.Clean(start)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repo root not found from %s", start)
		}
		dir = parent
	}
}

// mergeEnv constructs the final env map according to precedence.
// 1. OS env                      (highest)
// 2. repo .env                   (only if key is still unset)
// 3. script .env                 (override)
// 4. explicit overrides (payload) (override)
func mergeEnv(repoEnv, scriptEnv, payload map[string]string) map[string]string {
	out := map[string]string{}

	// 1. OS env
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		out[parts[0]] = parts[1]
	}

	// 2. repo defaults (only if absent)
	for k, v := range repoEnv {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}

	// 3. script overrides
	for k, v := range scriptEnv {
		out[k] = v
	}

	// 4. payload overrides (highest among files)
	for k, v := range payload {
		out[k] = v
	}

	return out
}

// mapToEnv converts map[string]string → []string{"k=v"} for exec.Cmd.Env.
func mapToEnv(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, fmt.Sprintf("%s=%s", k, v))
	}
	return out
}

// runCmd is a small wrapper to exec.CommandContext that proxies stdio.
func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// =============================================================================
// ScriptRunner
// =============================================================================

type ScriptRunner struct {
	nc         *nats.Conn
	js         jetstream.JetStream
	publisher  *messages.Publisher
	rootDir    string // where the repo‑level .env lives
	scriptsDir string
	langs      map[string]LangImpl
	jobs       sync.Map // jobID → cancelFunc
}

type jobState struct {
	cancel context.CancelFunc
}

// LangImpl defines language‑specific init & run hooks.
// Run receives a *merged* env map.

type LangImpl interface {
	Init(ctx context.Context, dir string) error
	Run(ctx context.Context, dir string, args []string, env map[string]string) *exec.Cmd
}

// NewScriptRunner sets up the runner, language adapters, and loads repo .env so
// the runner itself (logs, JetStream config, etc.) can consume those vars.
func NewScriptRunner(nc *nats.Conn, js jetstream.JetStream, scriptsDir string) *ScriptRunner {
	root, _ := repoRoot(scriptsDir)                // ignore error → empty string
	_ = godotenv.Load(filepath.Join(root, ".env")) // repo‑wide defaults for the runner

	langs := map[string]LangImpl{
		"python":     pythonImpl{},
		"typescript": tsImpl{},
	}

	return &ScriptRunner{
		nc:         nc,
		js:         js,
		publisher:  messages.NewPublisher(js),
		rootDir:    root,
		scriptsDir: scriptsDir,
		langs:      langs,
	}
}

// -----------------------------------------------------------------------------
// lifecycle
// -----------------------------------------------------------------------------

func (sr *ScriptRunner) Start(ctx context.Context) error {
	if err := sr.setupConsumer(ctx, "SCRIPT_CREATE", messages.ScriptCreateSubject, sr.handleCreate); err != nil {
		return err
	}
	if err := sr.setupConsumer(ctx, "SCRIPT_RUN", messages.ScriptRunSubject, sr.handleRun); err != nil {
		return err
	}
	go func() { <-ctx.Done(); sr.stopAllJobs() }()
	return nil
}

func (sr *ScriptRunner) setupConsumer(ctx context.Context, name, subject string, handler func(context.Context, jetstream.Msg)) error {
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
	_, err = consumer.Consume(func(msg jetstream.Msg) { handler(ctx, msg) })
	return err
}

// -----------------------------------------------------------------------------
// command.script.create – also runs schema→type code‑gen
// -----------------------------------------------------------------------------

func (sr *ScriptRunner) handleCreate(ctx context.Context, msg jetstream.Msg) {
	slog.Info("Received script create command", "payload", string(msg.Data()))

	var in messages.ScriptCreateCommand
	if err := json.Unmarshal(msg.Data(), &in); err != nil {
		slog.Error("Invalid create payload", "err", err)
		_ = msg.Ack()
		return
	}

	dir := filepath.Join(sr.scriptsDir, in.ScriptName)
	slog.Info("Creating script directory", "name", in.ScriptName, "type", in.ScriptType, "dir", dir)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("Failed to create script directory", "dir", dir, "err", err)
		evt := messages.NewScriptCreateErrorEvent(in.ScriptName, err.Error()).WithCorrelation(in.CorrelationID)
		_ = sr.publisher.PublishEvent(ctx, evt)
		_ = msg.Ack()
		return
	}

	impl := sr.langs[in.ScriptType]
	if impl == nil {
		slog.Error("Unsupported script type", "script_type", in.ScriptType)
		evt := messages.NewScriptCreateErrorEvent(in.ScriptName, "unsupported script_type").WithCorrelation(in.CorrelationID)
		_ = sr.publisher.PublishEvent(ctx, evt)
		_ = msg.Ack()
		return
	}

	if err := impl.Init(ctx, dir); err != nil {
		slog.Error("Script initialization failed", "name", in.ScriptName, "err", err)
		evt := messages.NewScriptCreateErrorEvent(in.ScriptName, err.Error()).WithCorrelation(in.CorrelationID)
		_ = sr.publisher.PublishEvent(ctx, evt)
		_ = msg.Ack()
		return
	}

	// --- JSON‑Schema → static types --------------------------------------
	if err := sr.codegenSchemas(ctx, dir, in.ScriptType); err != nil {
		slog.Error("Schema code‑gen failed", "script", in.ScriptName, "err", err)
		evt := messages.NewScriptCreateErrorEvent(in.ScriptName, err.Error()).WithCorrelation(in.CorrelationID)
		_ = sr.publisher.PublishEvent(ctx, evt)
		_ = msg.Ack()
		return
	}

	evt := messages.NewScriptCreatedEvent(in.ScriptName, in.ScriptType).WithCorrelation(in.CorrelationID)
	_ = sr.publisher.PublishEvent(ctx, evt)
	_ = msg.Ack()
}

// -----------------------------------------------------------------------------
// command.script.run – validates input & output against schema
// -----------------------------------------------------------------------------

func (sr *ScriptRunner) handleRun(ctx context.Context, msg jetstream.Msg) {
	var in messages.ScriptRunCommand // modified struct: {ScriptName, Input, Env, CorrelationID}
	if err := json.Unmarshal(msg.Data(), &in); err != nil {
		slog.Error("Invalid run payload", "err", err)
		_ = msg.Ack()
		return
	}

	scriptName := in.ScriptName
	dir := filepath.Join(sr.scriptsDir, scriptName)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		evt := messages.NewScriptJobErrorEvent(scriptName, "script not found").WithCorrelation(in.CorrelationID)
		_ = sr.publisher.PublishEvent(ctx, evt)
		_ = msg.Ack()
		return
	}

	// ---- validate input JSON --------------------------------------------
	schemaIn := filepath.Join(dir, "in.schema.json")
	if err := sr.validateJSON(schemaIn, in.Input); err != nil {
		evt := messages.NewScriptJobErrorEvent(scriptName, "input schema violation: "+err.Error()).WithCorrelation(in.CorrelationID)
		_ = sr.publisher.PublishEvent(ctx, evt)
		_ = msg.Ack()
		return
	}

	// ---- env layering ----------------------------------------------------
	repoEnv, _ := godotenv.Read(filepath.Join(sr.rootDir, ".env"))
	scriptEnv, _ := godotenv.Read(filepath.Join(dir, ".env"))
	envMap := mergeEnv(repoEnv, scriptEnv, in.Env)

	scriptType := sr.detectScriptType(dir)
	impl := sr.langs[scriptType]
	if impl == nil {
		evt := messages.NewScriptJobErrorEvent(scriptName, "unknown script type").WithCorrelation(in.CorrelationID)
		_ = sr.publisher.PublishEvent(ctx, evt)
		_ = msg.Ack()
		return
	}

	// write input to temp file for the script
	inputPath := filepath.Join(dir, ".tmp_input.json")
	if err := os.WriteFile(inputPath, in.Input, 0o644); err != nil {
		evt := messages.NewScriptJobErrorEvent(scriptName, err.Error()).WithCorrelation(in.CorrelationID)
		_ = sr.publisher.PublishEvent(ctx, evt)
		_ = msg.Ack()
		return
	}

	jobID := xid.New().String()
	jobCtx, cancel := context.WithCancel(ctx)
	sr.jobs.Store(jobID, jobState{cancel: cancel})

	cmd := impl.Run(jobCtx, dir, []string{inputPath}, envMap)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		evt := messages.NewScriptJobExitEvent(scriptName, jobID, -1).WithError(err.Error()).WithCorrelation(in.CorrelationID)
		_ = sr.publisher.PublishEvent(ctx, evt)
		cancel()
		sr.jobs.Delete(jobID)
		_ = msg.Ack()
		return
	}

	evt := messages.NewScriptJobStartedEvent(scriptName, jobID, cmd.Process.Pid).WithCorrelation(in.CorrelationID)
	_ = sr.publisher.PublishEvent(ctx, evt)

	go sr.pumpOutput(jobCtx, stdout, dir, scriptName, jobID, "stdout", in.CorrelationID)
	go sr.pumpOutput(jobCtx, stderr, dir, scriptName, jobID, "stderr", in.CorrelationID)
	go sr.waitForExit(ctx, cmd, scriptName, jobID, cancel, in.CorrelationID)

	_ = msg.Ack()
}

// detectScriptType looks for canonical files to decide which LangImpl to use.
func (sr *ScriptRunner) detectScriptType(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "main.py")); err == nil {
		return "python"
	}
	if _, err := os.Stat(filepath.Join(dir, "index.ts")); err == nil {
		return "typescript"
	}
	return ""
}

// waitForExit captures process termination and publishes exit event.
func (sr *ScriptRunner) waitForExit(ctx context.Context, cmd *exec.Cmd, scriptName, jobID string, cancel context.CancelFunc, correlationID string) {
	err := cmd.Wait()
	exitCode := 0
	var exitError string
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			exitError = err.Error()
		}
		slog.Error("Script process exited with error", "script", scriptName, "job_id", jobID, "code", exitCode, "err", err)
	} else {
		slog.Info("Script process completed successfully", "script", scriptName, "job_id", jobID)
	}

	evt := messages.NewScriptJobExitEvent(scriptName, jobID, exitCode).WithCorrelation(correlationID)
	if exitError != "" {
		evt = evt.WithError(exitError)
	}
	_ = sr.publisher.PublishEvent(ctx, evt)

	cancel()
	sr.jobs.Delete(jobID)
}

// pumpOutput streams each line of stdout/stderr as events and handles ##DATA## lines.
func (sr *ScriptRunner) pumpOutput(ctx context.Context, r io.Reader, dir, scriptName, jobID, stream, correlationID string) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Structured data line
		if strings.HasPrefix(line, "##DATA##") {
			payload := strings.TrimPrefix(line, "##DATA##")
			if err := sr.validateJSON(filepath.Join(dir, "out.schema.json"), []byte(payload)); err != nil {
				slog.Error("output schema violation", "script", scriptName, "job", jobID, "err", err)
				continue
			}
			evt := messages.NewScriptJobDataEvent(scriptName, jobID, []byte(payload)).WithCorrelation(correlationID)
			_ = sr.publisher.PublishEvent(ctx, evt)
			continue
		}

		// Regular stdout/stderr line
		evt := messages.NewScriptJobOutputEvent(scriptName, jobID, stream, line).WithCorrelation(correlationID)
		_ = sr.publisher.PublishEvent(ctx, evt)

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// stopAllJobs cancels any running scripts when the runner shuts down.
func (sr *ScriptRunner) stopAllJobs() {
	sr.jobs.Range(func(key, value any) bool {
		if js, ok := value.(jobState); ok {
			js.cancel()
		}
		sr.jobs.Delete(key)
		return true
	})
}

// =============================================================================
// JSON‑Schema helpers
// =============================================================================

func (sr *ScriptRunner) validateJSON(schemaPath string, data []byte) error {
	compiled, err := jsonschema.Compile("file://" + schemaPath)
	if err != nil {
		return err
	}
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	return compiled.Validate(v)
}

// =============================================================================
// Code‑generation: JSON‑Schema → TS / Python types
// =============================================================================

func (sr *ScriptRunner) codegenSchemas(ctx context.Context, dir, lang string) error {
	inSchema := filepath.Join(dir, "in.schema.json")
	outSchema := filepath.Join(dir, "out.schema.json")
	typesDir := filepath.Join(dir, "types")
	if err := os.MkdirAll(typesDir, 0o755); err != nil {
		return err
	}

	switch lang {
	case "typescript":
		if err := runCmd(ctx, "npx", "json-schema-to-typescript", inSchema, "-o", filepath.Join(typesDir, "in.ts"), "--bannerComment", ""); err != nil {
			return err
		}
		if err := runCmd(ctx, "npx", "json-schema-to-typescript", outSchema, "-o", filepath.Join(typesDir, "out.ts"), "--bannerComment", ""); err != nil {
			return err
		}
	case "python":
		if err := runCmd(ctx, "datamodel-codegen", "-i", inSchema, "-o", filepath.Join(typesDir, "in.py"), "--class-name", "Input"); err != nil {
			return err
		}
		if err := runCmd(ctx, "datamodel-codegen", "-i", outSchema, "-o", filepath.Join(typesDir, "out.py"), "--class-name", "Output"); err != nil {
			return err
		}
	}
	return nil
}

// =============================================================================
// Language adapters – Python
// =============================================================================

type pythonImpl struct{}

func (pythonImpl) Init(ctx context.Context, dir string) error {
	return runCmd(ctx, "uv", "init", dir)
}

func (pythonImpl) Run(ctx context.Context, dir string, args []string, env map[string]string) *exec.Cmd {
	// args[0] is path to input JSON file
	cmdStr := fmt.Sprintf("uv venv && uv pip install . && uv run main.py %s", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = dir
	cmd.Env = mapToEnv(env)
	return cmd
}

// =============================================================================
// Language adapters – TypeScript (bun)
// =============================================================================

type tsImpl struct{}

func (tsImpl) Init(ctx context.Context, dir string) error {
	return runCmd(ctx, "bun", "init", "-y")
}

func (tsImpl) Run(ctx context.Context, dir string, args []string, env map[string]string) *exec.Cmd {
	cmdStr := "bun install && bun run index.ts " + strings.Join(args, " ")
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = dir
	cmd.Env = mapToEnv(env)
	return cmd
}
