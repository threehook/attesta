package adl

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// segmentBytes is the size at which the record file moves on to a new segment.
const segmentBytes = 64 << 20

// segmentPath names segment seq of the record file base: base.000001, base.000002, and so on.
func segmentPath(base string, seq int) string { return fmt.Sprintf("%s.%06d", base, seq) }

// listSegments returns the sequence numbers of the segments of base, ascending.
func listSegments(base string) ([]int, error) {
	entries, err := os.ReadDir(filepath.Dir(base))
	if err != nil {
		return nil, err
	}

	prefix := filepath.Base(base) + "."

	var seqs []int

	for _, e := range entries {
		digits, ok := strings.CutPrefix(e.Name(), prefix)
		if !ok || len(digits) != 6 || strings.Trim(digits, "0123456789") != "" {
			continue
		}

		if seq, err := strconv.Atoi(digits); err == nil && seq > 0 {
			seqs = append(seqs, seq)
		}
	}

	sort.Ints(seqs)

	return seqs, nil
}

// segmentWriter appends records to the newest segment and fsyncs after each, so a record that Write reported is on disk. A full segment is closed
// and the next one started; a segment is never written to again once it has a successor.
type segmentWriter struct {
	mu     sync.Mutex
	base   string
	max    int64
	f      *os.File
	seq    int
	size   int64
	closed bool
}

func openSegments(base string, limit int64) (*segmentWriter, error) {
	if base == "" {
		return nil, errors.New("adl: the file output needs ATTESTA_ADL_FILE")
	}

	seqs, err := listSegments(base)
	if err != nil {
		return nil, fmt.Errorf("adl: list the record files: %w", err)
	}

	if len(seqs) == 0 {
		// A record file from before segments becomes the first one.
		if err := os.Rename(base, segmentPath(base, 1)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("adl: adopt the record file: %w", err)
		}

		seqs = []int{1}
	}

	w := &segmentWriter{base: base, max: limit, seq: seqs[len(seqs)-1]}
	if err := w.open(); err != nil {
		return nil, err
	}

	return w, nil
}

func (w *segmentWriter) open() error {
	f, err := os.OpenFile(segmentPath(w.base, w.seq), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("adl: open the record file: %w", err)
	}

	info, err := f.Stat()
	if err == nil {
		err = syncDir(filepath.Dir(w.base)) // a new file is only durable once its directory entry is
	}

	if err != nil {
		f.Close()
		return fmt.Errorf("adl: open the record file: %w", err)
	}

	w.f, w.size = f, info.Size()

	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer dir.Close()

	return dir.Sync()
}

func (w *segmentWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, errors.New("adl: the record file is closed")
	}

	if w.f != nil && w.size > 0 && w.size+int64(len(p)) > w.max {
		err := w.f.Close()
		w.f = nil
		w.seq++

		if err != nil {
			return 0, fmt.Errorf("adl: close the record file: %w", err)
		}
	}

	if w.f == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}

	n, err := w.f.Write(p)

	w.size += int64(n)
	if err != nil {
		return n, err
	}

	return n, w.f.Sync()
}

func (w *segmentWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.closed = true
	if w.f == nil {
		return nil
	}

	err := w.f.Close()
	w.f = nil

	return err
}
