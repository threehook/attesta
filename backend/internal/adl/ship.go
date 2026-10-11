package adl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

const (
	shipBatch    = 256
	shipTimeout  = 10 * time.Second
	shipPoll     = 5 * time.Second
	shipMaxDelay = 30 * time.Second
	keepShipped  = 24 * time.Hour
)

// capture keeps the records the OpenTelemetry logger builds, so the shipper can hand them to the exporter itself and see whether it succeeded.
type capture struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (c *capture) OnEmit(_ context.Context, r *sdklog.Record) error {
	c.mu.Lock()
	c.records = append(c.records, r.Clone())
	c.mu.Unlock()
	return nil
}
func (c *capture) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }
func (c *capture) Shutdown(context.Context) error                         { return nil }
func (c *capture) ForceFlush(context.Context) error                       { return nil }

func (c *capture) take() []sdklog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.records
	c.records = nil
	return out
}

// shipper sends the records in the record file over OTLP and remembers how far it got (segment and offset) in a file beside it, so a collector
// outage or a restart loses nothing: whatever the collector has not accepted is sent again. Delivery is at least once. A segment that is sent in
// full is kept for keep, then deleted.
type shipper struct {
	path, offsetPath string
	keep             time.Duration
	exporter         sdklog.Exporter
	capture          *capture
	otel             *otelState
	log              *slog.Logger
	wake             chan struct{}
	cancel           context.CancelFunc
	done             chan struct{}
}

func newShipper(path string, exporter sdklog.Exporter, resource map[string]string, log *slog.Logger) *shipper {
	c := &capture{}
	return &shipper{
		path: path, offsetPath: path + ".offset", keep: keepShipped, exporter: exporter, capture: c, otel: newOTelStateWith(c, resource), log: log,
		wake: make(chan struct{}, 1),
	}
}

// start runs the shipping loop until stop.
func (s *shipper) start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	go s.run(ctx)
}

// notify tells the loop a record was appended.
func (s *shipper) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// stop ends the loop, makes a last attempt to send what is left within ctx, and closes the exporter.
func (s *shipper) stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
	_, drainErr := s.drain(ctx)
	return errors.Join(drainErr, s.exporter.Shutdown(ctx))
}

func (s *shipper) run(ctx context.Context) {
	defer close(s.done)
	var delay time.Duration
	for {
		if _, err := s.drain(ctx); err != nil && ctx.Err() == nil {
			delay = min(max(2*delay, time.Second), shipMaxDelay)
			s.log.Error("adl: sending records to the collector failed, will retry", "error", err, "retry_in", delay)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			continue
		}
		delay = 0
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-time.After(shipPoll):
		}
	}
}

// drain sends batches until everything is sent or a send fails, then deletes the segments that were sent long enough ago.
func (s *shipper) drain(ctx context.Context) (int, error) {
	total := 0
	for {
		n, more, err := s.shipOnce(ctx)
		total += n
		if err != nil {
			return total, err
		}
		if !more {
			break
		}
	}
	s.removeOld()
	return total, nil
}

// position is how far the shipper got: the segment it is in and the offset in it of the first line not yet accepted.
type position struct {
	seq int
	off int64
}

// shipOnce sends up to one batch of complete lines after the saved position and moves the position past them once the exporter accepted them;
// at the end of a segment that has a successor it moves to the successor. A line that is not a record is skipped, or it would block everything
// behind it. more says whether the position moved.
func (s *shipper) shipOnce(ctx context.Context) (sent int, more bool, err error) {
	seqs, err := listSegments(s.path)
	if err != nil {
		return 0, false, fmt.Errorf("list the record files: %w", err)
	}
	if len(seqs) == 0 {
		return 0, false, nil
	}
	pos, ok := s.readPosition()
	if ok && !slices.Contains(seqs, pos.seq) {
		s.log.Error("adl: the saved position is in a record file that is gone, sending from the oldest", "segment", pos.seq)
		ok = false
	}
	if !ok {
		pos = position{seq: seqs[0]}
	}
	f, err := os.Open(segmentPath(s.path, pos.seq))
	if err != nil {
		return 0, false, fmt.Errorf("open the record file: %w", err)
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && pos.off > info.Size() {
		s.log.Error("adl: the record file is shorter than the saved offset, sending it from the start", "offset", pos.off, "size", info.Size())
		pos.off = 0
	}
	if _, err := f.Seek(pos.off, io.SeekStart); err != nil {
		return 0, false, fmt.Errorf("seek in the record file: %w", err)
	}
	r := bufio.NewReader(f)
	end := pos.off
	var batch []Record
	for len(batch) < shipBatch {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // end of the file, or a line still being written
		}
		end += int64(len(line))
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			s.log.Error("adl: skipping a line in the record file that is not a record", "segment", pos.seq, "offset", end-int64(len(line)), "error", err)
			continue
		}
		batch = append(batch, rec)
	}
	if end == pos.off {
		if pos.seq == seqs[len(seqs)-1] {
			return 0, false, nil
		}
		// Sent in full and the writer has moved on. The change time marks when it was sent, which is what the retention counts from.
		now := time.Now()
		if err := os.Chtimes(segmentPath(s.path, pos.seq), now, now); err != nil {
			return 0, false, fmt.Errorf("mark the record file as sent: %w", err)
		}
		next := seqs[slices.Index(seqs, pos.seq)+1]
		return 0, true, s.writePosition(position{seq: next})
	}
	if len(batch) > 0 {
		ctx, cancel := context.WithTimeout(ctx, shipTimeout)
		defer cancel()
		for _, rec := range batch {
			if err := s.otel.emit(ctx, rec); err != nil {
				return 0, false, err
			}
		}
		if err := s.exporter.Export(ctx, s.capture.take()); err != nil {
			return 0, false, fmt.Errorf("export the records: %w", err)
		}
	}
	return len(batch), true, s.writePosition(position{seq: pos.seq, off: end})
}

// removeOld deletes the segments before the saved position that were sent more than keep ago.
func (s *shipper) removeOld() {
	pos, ok := s.readPosition()
	if !ok {
		return
	}
	seqs, err := listSegments(s.path)
	if err != nil {
		return
	}
	for _, seq := range seqs {
		if seq >= pos.seq {
			break
		}
		path := segmentPath(s.path, seq)
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > s.keep {
			if err := os.Remove(path); err != nil {
				s.log.Error("adl: removing a sent record file failed", "segment", seq, "error", err)
			}
		}
	}
}

// readPosition reads the saved position. A file holding one number comes from before segments and is an offset in the first segment.
func (s *shipper) readPosition() (position, bool) {
	data, err := os.ReadFile(s.offsetPath)
	if err != nil {
		return position{}, false
	}
	fields := strings.Fields(string(data))
	nums := make([]int64, len(fields))
	for i, f := range fields {
		if nums[i], err = strconv.ParseInt(f, 10, 64); err != nil || nums[i] < 0 {
			return position{}, false
		}
	}
	switch len(nums) {
	case 1:
		return position{seq: 1, off: nums[0]}, true
	case 2:
		return position{seq: int(nums[0]), off: nums[1]}, true
	}
	return position{}, false
}

// writePosition replaces the position file whole, so a crash leaves the old or the new position, never half of one.
func (s *shipper) writePosition(pos position) error {
	tmp := s.offsetPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("write the position: %w", err)
	}
	_, err = fmt.Fprintf(f, "%d %d", pos.seq, pos.off)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, s.offsetPath)
	}
	if err != nil {
		return fmt.Errorf("write the position: %w", err)
	}
	return nil
}
