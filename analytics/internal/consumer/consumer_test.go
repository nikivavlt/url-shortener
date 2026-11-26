package consumer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/nikivavlt/url-shortener/analytics/internal/domain"
)

var errBoom = errors.New("boom")

// eventLog records the order of insert/commit calls.
type eventLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *eventLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, s)
}

func (l *eventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

// fakeReader behaves like kafka-go: returns queued messages, then blocks until ctx is done.
type fakeReader struct {
	mu        sync.Mutex
	queue     []kafka.Message
	committed [][]kafka.Message
	commitErr error
	log       *eventLog
}

func (f *fakeReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	f.mu.Lock()
	if len(f.queue) > 0 {
		m := f.queue[0]
		f.queue = f.queue[1:]
		f.mu.Unlock()
		return m, nil
	}
	f.mu.Unlock()
	<-ctx.Done()
	return kafka.Message{}, ctx.Err()
}

func (f *fakeReader) CommitMessages(_ context.Context, msgs ...kafka.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.commitErr != nil {
		return f.commitErr
	}
	f.committed = append(f.committed, msgs)
	f.log.add("commit")
	return nil
}

func (f *fakeReader) commits() [][]kafka.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]kafka.Message(nil), f.committed...)
}

type fakeRepo struct {
	mu      sync.Mutex
	batches [][]domain.ClickEvent
	err     error
	log     *eventLog
}

func (r *fakeRepo) InsertBatch(_ context.Context, events []domain.ClickEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.batches = append(r.batches, events)
	r.log.add("insert")
	return nil
}

func (r *fakeRepo) inserted() [][]domain.ClickEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]domain.ClickEvent(nil), r.batches...)
}

func validMsg(offset int64, code string) kafka.Message {
	return kafka.Message{
		Offset: offset,
		Value:  []byte(`{"short_code":"` + code + `","user_id":"u1","clicked_at":"2026-11-26T10:00:00Z","ip":"1.2.3.4","user_agent":"ua","referer":"r"}`),
	}
}

func poisonMsg(offset int64) kafka.Message {
	return kafka.Message{Offset: offset, Value: []byte(`not json`)}
}

func newFakes(msgs ...kafka.Message) (*fakeReader, *fakeRepo, *eventLog) {
	l := &eventLog{}
	return &fakeReader{queue: msgs, log: l}, &fakeRepo{log: l}, l
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

// start runs the consumer in the background; the returned stop cancels it and returns Run's error.
func start(t *testing.T, c *Consumer) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	t.Cleanup(cancel)
	return func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return after cancel")
			return nil
		}
	}
}

func TestRun_FullBatchInsertedThenCommitted(t *testing.T) {
	reader, repo, l := newFakes(validMsg(1, "a"), validMsg(2, "b"), validMsg(3, "c"))
	stop := start(t, New(reader, repo, 3, time.Hour))

	waitFor(t, func() bool { return len(reader.commits()) == 1 })
	if err := stop(); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}

	if got := repo.inserted(); len(got) != 1 || len(got[0]) != 3 {
		t.Fatalf("inserted batches = %+v, want one batch of 3", got)
	}
	if got := reader.commits(); len(got[0]) != 3 {
		t.Fatalf("committed %d messages, want 3", len(got[0]))
	}
	if got := l.snapshot(); len(got) != 2 || got[0] != "insert" || got[1] != "commit" {
		t.Fatalf("order = %v, want [insert commit]", got)
	}
}

func TestRun_PartialBatchFlushedOnTimer(t *testing.T) {
	reader, repo, _ := newFakes(validMsg(1, "a"), validMsg(2, "b"))
	stop := start(t, New(reader, repo, 100, 50*time.Millisecond))

	waitFor(t, func() bool { return len(reader.commits()) == 1 })
	if err := stop(); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}
	if got := repo.inserted(); len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("inserted batches = %+v, want one batch of 2", got)
	}
}

func TestRun_PoisonMessageSkippedButCommitted(t *testing.T) {
	reader, repo, _ := newFakes(validMsg(1, "a"), poisonMsg(2))
	stop := start(t, New(reader, repo, 100, 50*time.Millisecond))

	waitFor(t, func() bool { return len(reader.commits()) == 1 })
	if err := stop(); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}
	if got := repo.inserted(); len(got) != 1 || len(got[0]) != 1 || got[0][0].ShortCode != "a" {
		t.Fatalf("inserted = %+v, want only the valid event", got)
	}
	if got := reader.commits(); len(got[0]) != 2 {
		t.Fatalf("committed %d messages, want 2 (poison offset included)", len(got[0]))
	}
}

func TestRun_InsertFailureReturnsErrorWithoutCommit(t *testing.T) {
	reader, repo, _ := newFakes(validMsg(1, "a"), validMsg(2, "b"))
	repo.err = errBoom

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := New(reader, repo, 2, time.Hour).Run(ctx)

	if !errors.Is(err, errBoom) {
		t.Fatalf("Run returned %v, want errBoom", err)
	}
	if got := reader.commits(); len(got) != 0 {
		t.Fatalf("committed %d times after failed insert, want 0", len(got))
	}
}

func TestRun_CommitFailureReturnsError(t *testing.T) {
	reader, repo, _ := newFakes(validMsg(1, "a"))
	reader.commitErr = errBoom

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := New(reader, repo, 1, time.Hour).Run(ctx)

	if !errors.Is(err, errBoom) {
		t.Fatalf("Run returned %v, want errBoom", err)
	}
}

func TestRun_ShutdownWhileIdleReturnsNil(t *testing.T) {
	reader, repo, _ := newFakes()
	stop := start(t, New(reader, repo, 100, time.Hour))

	if err := stop(); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}
	if got := repo.inserted(); len(got) != 0 {
		t.Fatalf("unexpected inserts: %+v", got)
	}
}
