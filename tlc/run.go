package tlc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aburan28/tlacuilo/ast"
	"github.com/aburan28/tlacuilo/cfg"
	"github.com/aburan28/tlacuilo/parser"
)

// JarDownloadURL is the release asset used by DownloadJar.
const JarDownloadURL = "https://github.com/tlaplus/tlaplus/releases/latest/download/tla2tools.jar"

// ErrJarNotFound is returned when tla2tools.jar cannot be located.
var ErrJarNotFound = errors.New(
	"tla2tools.jar not found: set TLA2TOOLS_JAR, place tla2tools.jar in the " +
		"working directory, or call tlc.DownloadJar")

// FindJar locates tla2tools.jar: $TLA2TOOLS_JAR, ./tla2tools.jar,
// ~/.tlacuilo/tla2tools.jar, then common system java directories.
func FindJar() (string, error) {
	var candidates []string
	if p := os.Getenv("TLA2TOOLS_JAR"); p != "" {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, "tla2tools.jar")
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".tlacuilo", "tla2tools.jar"))
	}
	candidates = append(candidates,
		"/usr/local/share/java/tla2tools.jar",
		"/usr/share/java/tla2tools.jar",
	)
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", ErrJarNotFound
}

// DownloadJar fetches tla2tools.jar from the official TLA+ GitHub
// releases into dest.
func DownloadJar(ctx context.Context, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, JarDownloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading tla2tools.jar: HTTP %s", resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(dest), ".tla2tools-*.jar")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), dest)
}

// Options configures a TLC run. The zero value is usable: it locates the
// jar via FindJar and runs breadth-first model checking with deadlock
// checking enabled, one worker, and TLC's defaults.
type Options struct {
	// JavaPath overrides the java binary (default "java").
	JavaPath string
	// JarPath overrides jar discovery.
	JarPath string
	// JavaOpts are JVM options; a parallel-GC default is added when empty.
	JavaOpts []string

	// ConfigPath names the .cfg file; TLC defaults to <spec>.cfg.
	ConfigPath string
	// Workers sets -workers; 0 means 1, negative means "auto".
	Workers int
	// DisableDeadlockCheck passes -deadlock (which, in TLC's inverted
	// flag convention, turns deadlock checking off).
	DisableDeadlockCheck bool
	// Simulate runs simulation mode instead of exhaustive checking.
	Simulate bool
	// SimulateTraces bounds the number of simulation traces (num=N).
	SimulateTraces int64
	// Depth bounds trace length in simulation mode.
	Depth int
	// Seed fixes the simulation seed.
	Seed *int64
	// MetaDir sets -metadir, where TLC writes its state files.
	MetaDir string
	// ExtraArgs are appended verbatim to the TLC argument list.
	ExtraArgs []string

	// OnMessage, when set, receives each tool-mode message as it is
	// parsed, enabling progress streaming.
	OnMessage func(Message)
	// Stdout, when set, receives a copy of TLC's raw output.
	Stdout io.Writer
}

// Run model-checks the spec at specPath (a .tla file).
//
// The returned error is non-nil only when TLC could not be executed or
// its output could not be parsed; check Result.Status (or Result.Err)
// for the verification verdict.
func Run(ctx context.Context, specPath string, opts Options) (*Result, error) {
	jar := opts.JarPath
	if jar == "" {
		var err error
		if jar, err = FindJar(); err != nil {
			return nil, err
		}
	}
	javaBin := opts.JavaPath
	if javaBin == "" {
		javaBin = "java"
	}
	javaOpts := opts.JavaOpts
	if len(javaOpts) == 0 {
		javaOpts = []string{"-XX:+UseParallelGC"}
	}

	args := append([]string{}, javaOpts...)
	args = append(args, "-cp", jar, "tlc2.TLC", "-tool")
	if opts.ConfigPath != "" {
		args = append(args, "-config", opts.ConfigPath)
	}
	switch {
	case opts.Workers < 0:
		args = append(args, "-workers", "auto")
	case opts.Workers > 1:
		args = append(args, "-workers", strconv.Itoa(opts.Workers))
	}
	if opts.DisableDeadlockCheck {
		args = append(args, "-deadlock")
	}
	if opts.Simulate {
		sim := "-simulate"
		if opts.SimulateTraces > 0 {
			sim = fmt.Sprintf("-simulate num=%d", opts.SimulateTraces)
		}
		args = append(args, strings.Fields(sim)...)
	}
	if opts.Depth > 0 {
		args = append(args, "-depth", strconv.Itoa(opts.Depth))
	}
	if opts.Seed != nil {
		args = append(args, "-seed", strconv.FormatInt(*opts.Seed, 10))
	}
	if opts.MetaDir != "" {
		args = append(args, "-metadir", opts.MetaDir)
	}
	args = append(args, opts.ExtraArgs...)
	args = append(args, filepath.Base(specPath))

	cmd := exec.CommandContext(ctx, javaBin, args...)
	cmd.Dir = filepath.Dir(specPath)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout // TLC logs to stdout; merge stray stderr
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting TLC (is java installed?): %w", err)
	}

	var reader io.Reader = stdout
	if opts.Stdout != nil {
		reader = io.TeeReader(stdout, opts.Stdout)
	}
	msgs, parseErr := ParseToolOutput(reader, opts.OnMessage)

	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, waitErr
		}
	}
	if parseErr != nil {
		return nil, fmt.Errorf("parsing TLC output: %w", parseErr)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	r := NewResult(msgs, exitCode)
	r.Duration = time.Since(start)
	return r, nil
}

// Job describes a self-contained model-checking task whose inputs live
// in memory; Check writes them to a temporary directory and runs TLC.
type Job struct {
	// Module is the specification to check. Alternatively provide
	// Source (and ModuleName when it cannot be parsed from Source).
	Module *ast.Module
	Source string
	// ModuleName overrides the name used for the .tla file.
	ModuleName string
	// Config is the TLC configuration; required.
	Config *cfg.Config
	// AuxModules maps additional module names to their sources.
	AuxModules map[string]string
}

// Check writes the job's spec and config to a temp directory, runs TLC
// there, and cleans up. See Run for error semantics.
func Check(ctx context.Context, job Job, opts Options) (*Result, error) {
	src := job.Source
	name := job.ModuleName
	if job.Module != nil {
		src = job.Module.String()
		if name == "" {
			name = job.Module.Name
		}
	}
	if src == "" {
		return nil, errors.New("tlc: Job needs Module or Source")
	}
	if name == "" {
		m, err := parser.Parse(src)
		if err != nil {
			return nil, fmt.Errorf("tlc: cannot determine module name: %w", err)
		}
		name = m.Name
	}
	if job.Config == nil {
		return nil, errors.New("tlc: Job.Config is required")
	}
	if err := job.Config.Validate(); err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "tlacuilo-tlc-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	specPath := filepath.Join(dir, name+".tla")
	if err := os.WriteFile(specPath, []byte(src), 0o644); err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(dir, name+".cfg")
	if err := os.WriteFile(cfgPath, []byte(job.Config.Format()), 0o644); err != nil {
		return nil, err
	}
	for aux, auxSrc := range job.AuxModules {
		if err := os.WriteFile(filepath.Join(dir, aux+".tla"), []byte(auxSrc), 0o644); err != nil {
			return nil, err
		}
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = name + ".cfg"
	}
	if opts.MetaDir == "" {
		opts.MetaDir = filepath.Join(dir, "states")
	}
	return Run(ctx, specPath, opts)
}
