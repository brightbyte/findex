package recorder

import (
	"fmt"
	"io/fs"
	"syscall"
)

type Record struct {
	Path string
	Info fs.FileInfo
}

type Recorder struct{}

func New() *Recorder {
	return &Recorder{}
}

func (r *Recorder) Consume(in <-chan *Record) error {
	for rec := range in {
		err := r.Record(rec)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Recorder) Record(rec *Record) error {
	extra := ""
	if stat, ok := rec.Info.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
		extra = fmt.Sprintf("  %d links", stat.Nlink)
	}
	fmt.Printf("%s  %d bytes%s\n", rec.Path, rec.Info.Size(), extra)

	return nil
}
