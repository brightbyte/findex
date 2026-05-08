package main

import (
	"context"
	"findex/pkg/recorder"
	"findex/pkg/scanner"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"golang.org/x/sync/errgroup"
)

var includeDotFiles bool
var noRecursion bool
var dry bool
var quiet bool
var batchSize int
var dir string

func init() {
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, "warning: hard link detection is not supported on Windows")
	}

	flag.BoolVar(&includeDotFiles, "a", false, "include dot files and directories")
	flag.BoolVar(&includeDotFiles, "all", false, "include dot files and directories")

	flag.BoolVar(&noRecursion, "no-recursion", false, "do not recurse into subdirectories")
	flag.BoolVar(&noRecursion, "R", false, "do not recurse into subdirectories")

	flag.BoolVar(&dry, "dry", false, "print results to stdout instead of writing to database")
	flag.BoolVar(&quiet, "q", false, "suppress progress output")
	flag.BoolVar(&quiet, "quiet", false, "suppress progress output")
	flag.IntVar(&batchSize, "batch-size", 100, "number of inserts per transaction")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: findex [-a] [-R] [-dry] [-batch-size N] <directory>")
		os.Exit(1)
	}

	dir = flag.Arg(0)
}

func main() {
	var rec recorder.Recorder
	if dry {
		rec = &recorder.Echo{}
	} else {
		rec = &recorder.Sqlite{Filename: ".findex.sqlite", BatchSize: batchSize}
	}
	s := scanner.New()
	s.IncludeDotFiles = includeDotFiles
	s.Recursive = !noRecursion
	if !quiet {
		s.Output = os.Stdout
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	errs, ctx := errgroup.WithContext(ctx)

	channel := make(chan *recorder.Record)
	errs.Go(func() error {
		defer close(channel)
		err := s.Scan(ctx, dir, channel)
		return err
	})
	errs.Go(func() error {
		return recorder.Consume(dir, rec, channel)
	})

	if err := errs.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
