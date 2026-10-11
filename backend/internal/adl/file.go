package adl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// syncFile appends records and fsyncs after each, so a record that Write reported is on disk.
type syncFile struct{ f *os.File }

func openSyncFile(path string) (*syncFile, error) {
	if path == "" {
		return nil, errors.New("adl: the file output needs ATTESTA_ADL_FILE")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("adl: open the record file: %w", err)
	}
	// A new file is only durable once its directory entry is.
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		err = dir.Sync()
		dir.Close()
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("adl: sync the record directory: %w", err)
		}
	}
	return &syncFile{f: f}, nil
}

func (s *syncFile) Write(p []byte) (int, error) {
	n, err := s.f.Write(p)
	if err != nil {
		return n, err
	}
	return n, s.f.Sync()
}

func (s *syncFile) Close() error { return s.f.Close() }
