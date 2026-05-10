package scanner

import (
	"context"
	"findex/pkg/recorder"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"golang.org/x/sync/errgroup"
)

type Scanner struct {
	IncludeDotFiles bool
	Recursive       bool
	Output          io.Writer
}

func New() *Scanner {
	return &Scanner{
		Recursive: true,
	}
}

func (s *Scanner) ScanInto(ctx context.Context, dir string, rcrdr recorder.Recorder) error {
	errs, ctx := errgroup.WithContext(ctx)
	ch := make(chan *recorder.Record)
	errs.Go(func() error {
		defer close(ch)
		return s.Scan(ctx, dir, ch)
	})
	errs.Go(func() error {
		return recorder.Consume(dir, rcrdr, ch)
	})
	return errs.Wait()
}

func (s *Scanner) Scan(ctx context.Context, dir string, out chan<- *recorder.Record) error {
	var currentDir string
	var dirCount, total int

	flush := func(d string) {
		if s.Output != nil && dirCount > 0 {
			fmt.Fprintf(s.Output, "%s: %d files\n", d, dirCount)
		}
	}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if len(d.Name()) > 1 && d.Name()[0] == '.' {
			if strings.HasPrefix(d.Name(), ".findex.") || !s.IncludeDotFiles {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}

		if d.IsDir() {
			if !s.Recursive && path != dir {
				return fs.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		fileDir := filepath.Dir(relPath)
		if fileDir != currentDir {
			flush(currentDir)
			currentDir = fileDir
			dirCount = 0
		}

		select {
		case out <- &recorder.Record{Path: relPath, Info: info}:
		case <-ctx.Done():
			return ctx.Err()
		}

		dirCount++
		total++
		return nil
	})

	if err != nil {
		return err
	}

	flush(currentDir)
	if s.Output != nil {
		fmt.Fprintf(s.Output, "total: %d files\n", total)
	}
	return nil
}
