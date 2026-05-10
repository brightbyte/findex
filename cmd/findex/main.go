package main

import (
	"context"
	"findex/pkg/db"
	"findex/pkg/diff"
	"findex/pkg/dirdupes"
	"findex/pkg/dupes"
	"findex/pkg/list"
	"findex/pkg/recorder"
	"findex/pkg/scanner"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
)

var quiet bool

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.BoolVar(&quiet, "q", false, "quiet mode")
	fs.BoolVar(&quiet, "quiet", false, "suppress progress output")

	return fs
}

func init() {
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, "WARNING: Some operations may not work correctly on Windows")
	}
}

func main() {
	flag.Parse()
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
	case "dupes":
		cmdDupes(args)
	case "dirdupes":
		cmdDirDupes(args)
	case "diff":
		cmdDiff(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		cmdHelp()
		os.Exit(1)
	}
}

func cmdHelp() {
	fmt.Println(`usage: findex <command> [options] <directory>

Commands:
  update        scan a directory and record files to .findex.sqlite
  list          list contents of an existing .findex.sqlite
  dupes         find duplicates
  diff          show files added, removed, or changed since last update
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
  -g, -glob         filter output by glob pattern

dupes usage:
  findex dupes <directory>

dirdupes usage:
  findex dirdupes [-minsize N] <directory>

  Options:
  -minsize N    minimum directory size in KB to consider (default 1)

diff usage:
  findex diff [options] <directory>

  Options:
  -a, -all          include dot files and directories
  -R, -no-recursion do not recurse into subdirectories

Global Options:
  -q, -quiet    suppress progress output`)
}

func cmdUpdate(args []string) {
	fs := newFlagSet("update")
	var includeDotFiles bool
	var noRecursion bool
	var dry bool
	var batchSize int
	fs.BoolVar(&includeDotFiles, "a", false, "include dot files and directories")
	fs.BoolVar(&includeDotFiles, "all", false, "include dot files and directories")
	fs.BoolVar(&noRecursion, "no-recursion", false, "do not recurse into subdirectories")
	fs.BoolVar(&noRecursion, "R", false, "do not recurse into subdirectories")
	fs.BoolVar(&dry, "dry", false, "print results to stdout instead of writing to database")
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
		db, err := db.Open(filepath.Join(dir, ".findex.sqlite"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer db.Close()
		rec = &recorder.DBRecorder{DB: db, BatchSize: batchSize}
	}

	s := scanner.New()
	s.IncludeDotFiles = includeDotFiles
	s.Recursive = !noRecursion
	if !quiet {
		s.Output = os.Stdout
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	err := s.ScanInto(ctx, dir, rec)

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdDupes(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: findex dupes <directory>")
		os.Exit(1)
	}

	dir := args[0]
	db, err := db.Open(filepath.Join(dir, ".findex.sqlite"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()

	d := dupes.Detector{DB: db}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := d.Dupes(ctx, dir); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdDirDupes(args []string) {
	fs := newFlagSet("dirdupes")
	var minSizeKB int64
	fs.Int64Var(&minSizeKB, "minsize", 1, "minimum directory size in KB to consider")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: findex dirdupes [-minsize N] <directory>")
		os.Exit(1)
	}

	dir := fs.Arg(0)
	db, err := db.Open(filepath.Join(dir, ".findex.sqlite"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()

	d := dirdupes.NewDetector(db)
	d.MinSize = minSizeKB * 1024

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := d.DetectDupes(ctx, dir); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdDiff(args []string) {
	fs := newFlagSet("diff")
	var includeDotFiles bool
	var noRecursion bool
	fs.BoolVar(&includeDotFiles, "a", false, "include dot files and directories")
	fs.BoolVar(&includeDotFiles, "all", false, "include dot files and directories")
	fs.BoolVar(&noRecursion, "no-recursion", false, "do not recurse into subdirectories")
	fs.BoolVar(&noRecursion, "R", false, "do not recurse into subdirectories")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: findex diff [-a] [-R] <directory>")
		os.Exit(1)
	}

	dir := fs.Arg(0)
	db, err := db.Open(filepath.Join(dir, ".findex.sqlite"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()

	d := &diff.Differ{
		OldState:        db,
		IncludeDotFiles: includeDotFiles,
		Recursive:       !noRecursion,
		BatchSize:       500, // TODO: add option
	}
	if !quiet {
		d.Output = os.Stdout
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := d.Diff(ctx, dir); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdList(args []string) {
	fs := newFlagSet("list")
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

	db, err := db.Open(filepath.Join(dir, ".findex.sqlite"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()

	if err := list.List(db, prefix, glob); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
