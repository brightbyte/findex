package scanner

import (
	"context"
	"imkeeper/pkg/recorder"
	"io/fs"
	"path/filepath"
)

type Scanner struct {
	rec             *recorder.Recorder
	IncludeDotFiles bool
	Recursive       bool
}

func New() *Scanner {
	return &Scanner{
		IncludeDotFiles: false,
		Recursive:       true,
	}
}

func (s *Scanner) Scan(ctx context.Context, dir string, out chan<- *recorder.Record) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !s.IncludeDotFiles && len(d.Name()) > 1 && d.Name()[0] == '.' {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
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

		rec := &recorder.Record{path, info}
		out <- rec
		return nil
	})
}
