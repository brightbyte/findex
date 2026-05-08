package recorder

import (
	"io/fs"
)

type Record struct {
	Path string
	Info fs.FileInfo
}

type Recorder interface {
	Open(basepath string) error
	Record(*Record) error
	Close(err error) error
}

func Consume(basepath string, r Recorder, in <-chan *Record) error {
	err := r.Open(basepath)
	if err != nil {
		_ = r.Close(err)
		return err
	}

	for rec := range in {
		if err != nil {
			_ = r.Close(err)
			return err
		}
		err = r.Record(rec)
	}

	err = r.Close(err)
	return err
}
