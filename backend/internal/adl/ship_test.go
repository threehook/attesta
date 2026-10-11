package adl

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

type flakyExporter struct {
	memExporter
	down atomic.Bool
}

func (e *flakyExporter) Export(ctx context.Context, r []sdklog.Record) error {
	if e.down.Load() {
		return errors.New("collector down")
	}
	return e.memExporter.Export(ctx, r)
}

func (e *flakyExporter) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.records)
}

// fileLogger writes to a record file the way New does, without starting a shipper.
func fileLogger(t *testing.T) (*Logger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "adl.jsonl")
	f, err := openSyncFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	l := testLogger(f)
	return l, path
}

func newTestShipper(path string, exp sdklog.Exporter) *shipper {
	return newShipper(path, exp, map[string]string{"service.name": "attesta-adl"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func logN(t *testing.T, l *Logger, n int) {
	t.Helper()
	for range n {
		if err := l.Log(context.Background(), allowed()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheShipperSendsEachRecordOnceAndResumesAfterARestart(t *testing.T) {
	l, path := fileLogger(t)
	exp := &flakyExporter{}
	logN(t, l, 2)

	s := newTestShipper(path, exp)
	if n, err := s.drain(context.Background()); err != nil || n != 2 {
		t.Fatalf("first drain = %d, %v; want 2", n, err)
	}
	if n, _ := s.drain(context.Background()); n != 0 {
		t.Fatalf("second drain sent %d, want 0", n)
	}
	logN(t, l, 1)
	restarted := newTestShipper(path, exp)
	if n, err := restarted.drain(context.Background()); err != nil || n != 1 {
		t.Fatalf("drain after restart = %d, %v; want only the new record", n, err)
	}
	if exp.count() != 3 {
		t.Fatalf("exported %d records, want 3", exp.count())
	}
}

func TestRecordsTheCollectorMissedAreSentWhenItIsBack(t *testing.T) {
	l, path := fileLogger(t)
	exp := &flakyExporter{}
	exp.down.Store(true)
	logN(t, l, 2)

	s := newTestShipper(path, exp)
	if _, err := s.drain(context.Background()); err == nil {
		t.Fatal("want an error while the collector is down")
	}
	if s.readOffset() != 0 {
		t.Fatal("the offset moved although nothing was accepted")
	}
	exp.down.Store(false)
	if n, err := s.drain(context.Background()); err != nil || n != 2 {
		t.Fatalf("drain after recovery = %d, %v; want 2", n, err)
	}
}

func TestAHalfWrittenLineWaitsAndABrokenLineIsSkipped(t *testing.T) {
	l, path := fileLogger(t)
	exp := &flakyExporter{}
	logN(t, l, 1)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString("not json\n{\"event_name\":\"adl.access_eval")

	s := newTestShipper(path, exp)
	if n, err := s.drain(context.Background()); err != nil || n != 1 {
		t.Fatalf("drain = %d, %v; want the one record, with the broken line skipped and the half line left", n, err)
	}
	f.WriteString("uation\",\"timestamp\":1,\"status\":\"Ok\",\"body\":{}}\n")
	if n, err := s.drain(context.Background()); err != nil || n != 1 {
		t.Fatalf("drain after the line was finished = %d, %v; want 1", n, err)
	}
}

func TestStopSendsWhatIsLeftAndLogWakesTheLoop(t *testing.T) {
	l, path := fileLogger(t)
	exp := &flakyExporter{}
	l.ship = newTestShipper(path, exp)
	l.ship.start()

	logN(t, l, 1)
	deadline := time.Now().Add(2 * time.Second)
	for exp.count() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if exp.count() != 1 {
		t.Fatalf("exported %d records without waiting for the poll, want 1", exp.count())
	}

	logN(t, l, 1)
	if err := l.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if exp.count() != 2 {
		t.Fatalf("exported %d records after Close, want 2", exp.count())
	}
}
