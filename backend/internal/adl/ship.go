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

// shipper sends the records in the record file over OTLP and remembers how far it got in a file beside it, so a collector outage or a restart
// loses nothing: whatever the collector has not accepted is sent again. Delivery is at least once.
type shipper struct {
	path, offsetPath string
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
		path: path, offsetPath: path + ".offset", exporter: exporter, capture: c, otel: newOTelStateWith(c, resource), log: log,
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

// drain sends batches until the file is exhausted or a send fails.
func (s *shipper) drain(ctx context.Context) (int, error) {
	total := 0
	for {
		n, err := s.shipOnce(ctx)
		total += n
		if err != nil || n == 0 {
			return total, err
		}
	}
}

// shipOnce sends up to one batch of complete lines after the saved offset and moves the offset past them once the exporter accepted them. A line
// that is not a record is skipped, or it would block everything behind it.
func (s *shipper) shipOnce(ctx context.Context) (int, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return 0, fmt.Errorf("open the record file: %w", err)
	}
	defer f.Close()
	off := s.readOffset()
	if info, err := f.Stat(); err == nil && off > info.Size() {
		s.log.Error("adl: the record file is shorter than the saved offset, sending it from the start", "offset", off, "size", info.Size())
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seek in the record file: %w", err)
	}
	r := bufio.NewReader(f)
	end := off
	var batch []Record
	for len(batch) < shipBatch {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // end of the file, or a line still being written
		}
		end += int64(len(line))
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			s.log.Error("adl: skipping a line in the record file that is not a record", "offset", end-int64(len(line)), "error", err)
			continue
		}
		batch = append(batch, rec)
	}
	if end == off {
		return 0, nil
	}
	if len(batch) > 0 {
		ctx, cancel := context.WithTimeout(ctx, shipTimeout)
		defer cancel()
		for _, rec := range batch {
			if err := s.otel.emit(ctx, rec); err != nil {
				return 0, err
			}
		}
		if err := s.exporter.Export(ctx, s.capture.take()); err != nil {
			return 0, fmt.Errorf("export the records: %w", err)
		}
	}
	if err := s.writeOffset(end); err != nil {
		return len(batch), err
	}
	return len(batch), nil
}

func (s *shipper) readOffset() int64 {
	data, err := os.ReadFile(s.offsetPath)
	if err != nil {
		return 0
	}
	off, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || off < 0 {
		return 0
	}
	return off
}

// writeOffset replaces the offset file whole, so a crash leaves the old or the new offset, never half of one.
func (s *shipper) writeOffset(off int64) error {
	tmp := s.offsetPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("write the offset: %w", err)
	}
	_, err = f.WriteString(strconv.FormatInt(off, 10))
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
		return fmt.Errorf("write the offset: %w", err)
	}
	return nil
}
