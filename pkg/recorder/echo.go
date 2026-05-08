package recorder

import (
	"fmt"
	"syscall"
)

type Echo struct{}

func (r *Echo) Open(basepath string) error {
	// noop
	return nil
}

func (r *Echo) Close(err error) error {
	// noop
	return nil
}

func (r *Echo) Record(rec *Record) error {
	extra := ""
	if stat, ok := rec.Info.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
		extra = fmt.Sprintf("  %d links", stat.Nlink)
	}
	fmt.Printf("%s  %d bytes%s\n", rec.Path, rec.Info.Size(), extra)

	return nil
}
