package recorder

import (
	"fmt"
	"io/fs"
	"syscall"
)

type Recorder struct{}

func (r *Recorder) Record(path *string, info fs.FileInfo) {
	extra := ""
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
		extra = fmt.Sprintf("  %d links", stat.Nlink)
	}
	fmt.Printf("%s  %d bytes%s\n", *path, info.Size(), extra)
}
