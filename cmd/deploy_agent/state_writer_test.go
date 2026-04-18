package deploy_agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuigh/swirl/biz"
)

func TestStateWriterInitialFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	initial := &biz.SelfDeployState{JobID: "job1", Phase: biz.SelfDeployPhasePending}
	sw, err := newStateWriter(path, initial)
	if err != nil {
		t.Fatalf("newStateWriter: %v", err)
	}
	defer sw.Close()

	// File must exist immediately after construction.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st biz.SelfDeployState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.JobID != "job1" {
		t.Fatalf("expected jobId=job1, got %q", st.JobID)
	}
	if st.Phase != biz.SelfDeployPhasePending {
		t.Fatalf("expected phase pending, got %q", st.Phase)
	}
}

func TestStateWriterSetPhaseAndSucceed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	sw, err := newStateWriter(path, &biz.SelfDeployState{JobID: "job2"})
	if err != nil {
		t.Fatalf("newStateWriter: %v", err)
	}

	sw.SetPhase(biz.SelfDeployPhaseStarting)
	sw.Logf("hello %s", "world")
	sw.Succeed()
	sw.Close()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st biz.SelfDeployState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.Phase != biz.SelfDeployPhaseSuccess {
		t.Fatalf("expected success phase, got %q", st.Phase)
	}
	if st.FinishedAt.IsZero() {
		t.Fatalf("expected finishedAt set on Succeed()")
	}
	joined := strings.Join(st.LogTail, "\n")
	if !strings.Contains(joined, "hello world") {
		t.Fatalf("expected log to contain hello world, got %q", joined)
	}
	if !strings.Contains(joined, "SUCCESS") {
		t.Fatalf("expected log to contain SUCCESS marker, got %q", joined)
	}
}

func TestStateWriterFail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	sw, err := newStateWriter(path, &biz.SelfDeployState{JobID: "job3"})
	if err != nil {
		t.Fatalf("newStateWriter: %v", err)
	}
	defer sw.Close()

	sw.Fail(&testErr{"image pull bad"})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st biz.SelfDeployState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.Phase != biz.SelfDeployPhaseFailed {
		t.Fatalf("expected failed phase, got %q", st.Phase)
	}
	if st.Error != "image pull bad" {
		t.Fatalf("expected error image pull bad, got %q", st.Error)
	}
}

// TestStateWriterRingBufferWrap validates the ring buffer capacity
// bound: after more than logRingCapacity entries, the oldest ones are
// dropped and only the most recent logRingCapacity entries remain, in
// order.
func TestStateWriterRingBufferWrap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	sw, err := newStateWriter(path, &biz.SelfDeployState{JobID: "job4"})
	if err != nil {
		t.Fatalf("newStateWriter: %v", err)
	}
	defer sw.Close()

	total := logRingCapacity + 100
	for i := 0; i < total; i++ {
		sw.Logf("line-%d", i)
	}
	// Force immediate flush.
	sw.mu.Lock()
	_ = sw.flushLocked()
	sw.mu.Unlock()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st biz.SelfDeployState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(st.LogTail) != logRingCapacity {
		t.Fatalf("expected ring size %d, got %d", logRingCapacity, len(st.LogTail))
	}
	// Oldest entry should be line-100, newest should be line-(total-1).
	first := st.LogTail[0]
	last := st.LogTail[len(st.LogTail)-1]
	if !strings.Contains(first, "line-100") {
		t.Fatalf("expected oldest entry to be line-100, got %q", first)
	}
	if !strings.Contains(last, "line-"+itoa(total-1)) {
		t.Fatalf("expected newest entry to be line-%d, got %q", total-1, last)
	}
}

// TestStateWriterConcurrent spawns many goroutines writing logs and
// changing phases; the final flush must succeed without data races
// (run with -race to catch them).
func TestStateWriterConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	sw, err := newStateWriter(path, &biz.SelfDeployState{JobID: "job5"})
	if err != nil {
		t.Fatalf("newStateWriter: %v", err)
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				sw.Logf("g%d-line%d", g, i)
				if i%50 == 0 {
					sw.SetPhase(biz.SelfDeployPhaseStarting)
				}
			}
		}(g)
	}
	wg.Wait()
	sw.Succeed()
	sw.Close()

	// Must be readable as JSON.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st biz.SelfDeployState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.Phase != biz.SelfDeployPhaseSuccess {
		t.Fatalf("expected success, got %q", st.Phase)
	}
}

// TestStateWriterFlushesPeriodically verifies the background flusher
// picks up Logf calls within stateFlushInterval + a small margin.
func TestStateWriterFlushesPeriodically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	sw, err := newStateWriter(path, &biz.SelfDeployState{JobID: "job6", Phase: biz.SelfDeployPhasePending})
	if err != nil {
		t.Fatalf("newStateWriter: %v", err)
	}
	defer sw.Close()

	sw.Logf("waiting for periodic flush")
	// Wait a bit longer than stateFlushInterval to allow the background
	// goroutine to run.
	time.Sleep(stateFlushInterval + 500*time.Millisecond)

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st biz.SelfDeployState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	joined := strings.Join(st.LogTail, "\n")
	if !strings.Contains(joined, "waiting for periodic flush") {
		t.Fatalf("expected periodic flush to persist log, got %q", joined)
	}
}

// testErr is a trivial error type used by tests so we don't pull
// errors.New for every assertion.
type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

// itoa is a test-only helper to avoid importing strconv just for
// formatting.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var out []byte
	for i > 0 {
		out = append([]byte{byte('0' + i%10)}, out...)
		i /= 10
	}
	if neg {
		out = append([]byte{'-'}, out...)
	}
	return string(out)
}
