package main

import (
	"context"
	"flag"
	"fmt"
	"imkeeper/pkg/recorder"
	"imkeeper/pkg/scanner"
	"os"
	"runtime"

	"golang.org/x/sync/errgroup"
)

func main() {
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, "warning: hard link detection is not supported on Windows")
	}

	var includeDotFiles bool
	flag.BoolVar(&includeDotFiles, "a", false, "include dot files and directories")
	flag.BoolVar(&includeDotFiles, "all", false, "include dot files and directories")

	var noRecursion bool
	flag.BoolVar(&noRecursion, "no-recursion", false, "do not recurse into subdirectories")
	flag.BoolVar(&noRecursion, "R", false, "do not recurse into subdirectories")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: findex [-a] [-R] <directory>")
		os.Exit(1)
	}

	dir := flag.Arg(0)
	rec := recorder.New()
	s := scanner.New()
	s.IncludeDotFiles = includeDotFiles
	s.Recursive = !noRecursion

	ctx := context.Background()
	errs, ctx := errgroup.WithContext(ctx)

	channel := make(chan *recorder.Record)
	errs.Go(func() error {
		err := s.Scan(ctx, dir, channel)
		close(channel)
		return err
	})
	errs.Go(func() error {
		return rec.Consume(channel)
	})

	if err := errs.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
