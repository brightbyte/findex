package recorder

import "fmt"

type Recorder struct{}

func (r *Recorder) Record(path string, size int64) {
	fmt.Printf("%s  %d bytes\n", path, size)
}
