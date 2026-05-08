package main

import (
	"context"
	"findex/pkg/list"
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

var quiet bool

func init() {
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, "warning: hard link detection is not supported on Windows")
	}

	flag.BoolVar(&quiet, "q", false, "suppress progress output")
	flag.BoolVar(&quiet, "quiet", false, "suppress progress output")
	flag.Parse()
}

func main() {
	if flag.NArg() < 1 {
		cmdHelp()
		os.Exit(1)
	}

	cmd := flag.Arg(0)
	args := flag.Args()[1:]

	switch cmd {
	case "help":
		cmdHelp()
	case "update":
		cmdUpdate(args)
	case "list":
		cmdList(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		cmdHelp()
		os.Exit(1)
	}
}

func cmdHelp() {
	fmt.Println(`usage: findex [-q] <command> [options] <directory>

Global flags:
  -q, -quiet    suppress progress output

Commands:
  update        scan a directory and record files to .findex.sqlite
  list          list contents of an existing .findex.sqlite
  help          show this help

update usage:
  findex update [options] <directory>

  Options:
  -a, -all          include dot files and directories
  -R, -no-recursion do not recurse into subdirectories
  -dry              print results to stdout instead of writing to database
  -batch-size N     number of inserts per transaction (default 100)

list usage:
  findex list [options] <directory> [prefix]

  Options:
  -g, -glob         filter output by glob pattern`)
}

func cmdUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	var includeDotFiles bool
	var noRecursion bool
	var dry bool
	var batchSize int
	fs.BoolVar(&includeDotFiles, "a", false, "include dot files and directories")
	fs.BoolVar(&includeDotFiles, "all", false, "include dot files and directories")
	fs.BoolVar(&noRecursion, "no-recursion", false, "do not recurse into subdirectories")
	fs.BoolVar(&noRecursion, "R", false, "do not recurse into subdirectories")
	fs.BoolVar(&dry, "dry", false, "print results to stdout instead of writing to database")
	fs.BoolVar(&quiet, "q", false, "suppress progress output")
	fs.BoolVar(&quiet, "quiet", false, "suppress progress output")
	fs.IntVar(&batchSize, "batch-size", 100, "number of inserts per transaction")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: findex update [-a] [-R] [-dry] [-batch-size N] <directory>")
		os.Exit(1)
	}
	dir := fs.Arg(0)

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
		return s.Scan(ctx, dir, channel)
	})
	errs.Go(func() error {
		return recorder.Consume(dir, rec, channel)
	})

	if err := errs.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	var glob string
	fs.StringVar(&glob, "g", "", "filter by glob pattern on basename")
	fs.StringVar(&glob, "glob", "", "filter by glob pattern on basename")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: findex list [-g pattern] <directory> [prefix]")
		os.Exit(1)
	}
	dir := fs.Arg(0)
	prefix := ""
	if fs.NArg() >= 2 {
		prefix = fs.Arg(1)
	}

	if err := list.List(dir, prefix, glob); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
