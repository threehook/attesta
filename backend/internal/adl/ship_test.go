package adl

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
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
	f, err := openSegments(path, segmentBytes)
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
	if pos, _ := s.readPosition(); pos.off != 0 {
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
	f, err := os.OpenFile(segmentPath(path, 1), os.O_APPEND|os.O_WRONLY, 0o600)
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

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestRecordsAreShippedAcrossSegmentsInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adl.jsonl")
	w, err := openSegments(path, 600) // smaller than a record: one record per segment
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	l := testLogger(w)
	logN(t, l, 3)
	if seqs, _ := listSegments(path); len(seqs) != 3 {
		t.Fatalf("segments = %v, want 3", seqs)
	}

	exp := &flakyExporter{}
	s := newTestShipper(path, exp)
	if n, err := s.drain(context.Background()); err != nil || n != 3 || exp.count() != 3 {
		t.Fatalf("drain = %d, %v, exported %d; want 3", n, err, exp.count())
	}
	if pos, _ := s.readPosition(); pos.seq != 3 {
		t.Fatalf("position = %+v, want it in the last segment", pos)
	}
	logN(t, l, 1)
	if n, err := s.drain(context.Background()); err != nil || n != 1 {
		t.Fatalf("drain after a new segment = %d, %v; want 1", n, err)
	}
}

func TestASegmentIsKeptForADayAfterItWasSentThenDeleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adl.jsonl")
	w, err := openSegments(path, 600)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	l := testLogger(w)
	logN(t, l, 3)
	old := time.Now().Add(-48 * time.Hour)
	for seq := 1; seq <= 3; seq++ {
		os.Chtimes(segmentPath(path, seq), old, old)
	}

	exp := &flakyExporter{}
	exp.down.Store(true)
	s := newTestShipper(path, exp)
	s.drain(context.Background())
	for seq := 1; seq <= 3; seq++ {
		if !exists(segmentPath(path, seq)) {
			t.Fatalf("segment %d was deleted before it was sent", seq)
		}
	}

	exp.down.Store(false)
	if _, err := s.drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	for seq := 1; seq <= 2; seq++ {
		if !exists(segmentPath(path, seq)) {
			t.Fatalf("segment %d was deleted although it was sent just now, however old its records", seq)
		}
	}
	for seq := 1; seq <= 2; seq++ {
		os.Chtimes(segmentPath(path, seq), old, old)
	}
	os.Chtimes(segmentPath(path, 3), old, old)
	s.drain(context.Background())
	if exists(segmentPath(path, 1)) || exists(segmentPath(path, 2)) {
		t.Fatal("sent segments older than the retention are still there")
	}
	if !exists(segmentPath(path, 3)) {
		t.Fatal("the segment being written was deleted")
	}
}

func TestARecordFileFromBeforeSegmentsIsAdoptedWithItsOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adl.jsonl")
	legacy, err := openSegments(path, segmentBytes)
	if err != nil {
		t.Fatal(err)
	}
	logN(t, testLogger(legacy), 2)
	legacy.Close()
	size := fileSize(t, segmentPath(path, 1))
	if err := os.Rename(segmentPath(path, 1), path); err != nil { // the layout before segments
		t.Fatal(err)
	}
	os.WriteFile(path+".offset", []byte(strconv.FormatInt(size, 10)), 0o600)

	w, err := openSegments(path, segmentBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	logN(t, testLogger(w), 1)
	exp := &flakyExporter{}
	if n, err := newTestShipper(path, exp).drain(context.Background()); err != nil || n != 1 {
		t.Fatalf("drain = %d, %v; want only the record written after the upgrade", n, err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
