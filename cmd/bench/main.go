// Command bench runs the demo workflows on several runner types and summarizes GitHub's job timings.
// See docs/benchmarks.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/bench"
	"github.com/TeddyBertrand/Mayfly/internal/config"
	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/logkey"
)

const (
	defaultRepo       = "TeddyBertrand/mayfly-demo"
	defaultRef        = "main"
	defaultWorkflows  = "go-build,npm-test"
	defaultRunners    = "hosted,mayfly,classic"
	defaultWarmup     = 2
	defaultRuns       = 10
	defaultPoll       = 5 * time.Second
	defaultRunTimeout = 10 * time.Minute
	defaultTolerance  = 0.10
	// exitCompareFailed is the exit status when a cell is outside the tolerance, distinct from a usage or I/O error.
	exitCompareFailed = 2
	// resultsPerm lets others read the results; they hold timings and run IDs, nothing secret.
	resultsPerm = 0o644
	// compareFiles is how many CSV files compare takes: a baseline and a repetition.
	compareFiles = 2
	// tokenEnv holds the GitHub token; `gh auth token` prints one with the needed Actions write scope.
	tokenEnv = "GH_TOKEN"
)

// errOutsideTolerance reports a comparison where at least one cell moved more than allowed.
var errOutsideTolerance = errors.New("at least one cell is outside the tolerance")

// usage is printed for a missing or unknown subcommand.
const usage = `usage:
  bench run -out results.csv [flags]   dispatch the runs, write a CSV and print the summary
  bench summary results.csv            print the summary of a CSV
  bench compare a.csv b.csv [flags]    check that median totals moved by at most -tolerance`

// main dispatches to a subcommand and exits non-zero on failure.
func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := run(ctx, os.Args[1:], os.Stdout, logger)
	switch {
	case err == nil:
	case errors.Is(err, errOutsideTolerance):
		os.Exit(exitCompareFailed)
	default:
		logger.Error("bench failed", logkey.Err, err)
		os.Exit(1)
	}
}

// run executes the subcommand in args, writing tables to out.
func run(ctx context.Context, args []string, out io.Writer, logger *slog.Logger) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "run":
		return runBench(ctx, args[1:], out, logger)
	case "summary":
		return runSummary(args[1:], out)
	case "compare":
		return runCompare(args[1:], out)
	default:
		return fmt.Errorf("unknown subcommand %q\n%s", args[0], usage)
	}
}

// runBench parses the run flags, runs the benchmark into the CSV file and prints its summary.
func runBench(ctx context.Context, args []string, out io.Writer, logger *slog.Logger) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var (
		cfg                bench.Config
		workflows, runners string
		outPath, apiURL    string
	)
	fs.StringVar(&cfg.Repo, "repo", defaultRepo, "repository holding the demo workflows")
	fs.StringVar(&cfg.Ref, "ref", defaultRef, "git ref to run the workflows on")
	fs.StringVar(&workflows, "workflows", defaultWorkflows, "comma-separated workflow names (file name without .yml)")
	fs.StringVar(&runners, "runners", defaultRunners, "comma-separated values of the workflows' runner input")
	fs.IntVar(&cfg.Warmup, "warmup", defaultWarmup, "warm-up iterations per cell, recorded but not summarized")
	fs.IntVar(&cfg.Runs, "runs", defaultRuns, "measured iterations per cell")
	fs.DurationVar(&cfg.Poll, "poll", defaultPoll, "how often a run's status is checked")
	fs.DurationVar(&cfg.RunTimeout, "run-timeout", defaultRunTimeout, "give up on one run after this long")
	fs.StringVar(&outPath, "out", "", "CSV file to write (required, must not exist)")
	fs.StringVar(&apiURL, "api", config.DefaultGitHubAPIURL, "GitHub REST API base URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg.Workflows, cfg.Runners = splitList(workflows), splitList(runners)
	if outPath == "" {
		return errors.New("-out is required")
	}
	if cfg.Runs < 1 || cfg.Warmup < 0 || len(cfg.Workflows) == 0 || len(cfg.Runners) == 0 {
		return errors.New("need -runs >= 1, -warmup >= 0, and at least one workflow and runner")
	}
	token := os.Getenv(tokenEnv)
	if token == "" {
		return fmt.Errorf("%s is not set; run with %s=$(gh auth token)", tokenEnv, tokenEnv)
	}

	// O_EXCL: never overwrite an earlier benchmark's results.
	f, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, resultsPerm)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w, err := bench.NewWriter(f)
	if err != nil {
		return err
	}

	var recs []bench.Record
	h := &bench.Harness{API: github.NewTokenClient(apiURL, token), Logger: logger, Now: time.Now, Sleep: bench.SleepContext}
	runErr := h.Run(ctx, cfg, func(r bench.Record) error {
		recs = append(recs, r)
		return w.Write(r)
	})
	if err := bench.WriteSummary(out, bench.Summaries(recs)); err != nil {
		return err
	}
	return runErr
}

// runSummary prints the summary table of one CSV file.
func runSummary(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: bench summary results.csv")
	}
	recs, err := readFile(args[0])
	if err != nil {
		return err
	}
	return bench.WriteSummary(out, bench.Summaries(recs))
}

// runCompare prints how each cell's median total moved between two CSV files and fails outside the tolerance.
func runCompare(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	tolerance := fs.Float64("tolerance", defaultTolerance, "largest allowed change of a median total, as a fraction")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != compareFiles {
		return errors.New("usage: bench compare [-tolerance 0.10] a.csv b.csv")
	}
	a, err := readFile(fs.Arg(0))
	if err != nil {
		return err
	}
	b, err := readFile(fs.Arg(1))
	if err != nil {
		return err
	}
	ok, err := bench.WriteComparison(out, bench.Compare(bench.Summaries(a), bench.Summaries(b), *tolerance), *tolerance)
	if err != nil {
		return err
	}
	if !ok {
		return errOutsideTolerance
	}
	return nil
}

// readFile reads the records of the CSV file at path.
func readFile(path string) ([]bench.Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	recs, err := bench.ReadRecords(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return recs, nil
}

// splitList splits a comma-separated flag value, dropping empty items and spaces.
func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
