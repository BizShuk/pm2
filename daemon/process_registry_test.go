package daemon

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bizshuk/pm2/process"
)

// helper: build a ManagedProcess stub for testing.
func stub(name string) *ManagedProcess {
	return &ManagedProcess{
		Info: process.ProcessInfo{
			AppConfig: process.AppConfig{
				Namespace: "default",
				Name:      name,
			},
			ID:     0, // overwritten per-test
			Status: process.StatusOnline,
		},
	}
}

func TestRegistryBasicCRUD(t *testing.T) {
	r := NewProcessRegistry()

	if r.Len() != 0 {
		t.Fatalf("expected empty registry, got len=%d", r.Len())
	}

	// Add + Get
	mp := stub("appA")
	r.Add("default:appA", mp)

	got, ok := r.Get("default:appA")
	if !ok || got != mp {
		t.Fatalf("Get returned (%v, %v); want (%p, true)", got, ok, mp)
	}
	if r.Len() != 1 {
		t.Fatalf("after Add len=%d, want 1", r.Len())
	}

	// Get non-existent
	if _, ok := r.Get("does:not-exist"); ok {
		t.Fatal("Get('does:not-exist') returned ok=true, want false")
	}

	// Remove
	removed, ok := r.Remove("default:appA")
	if !ok || removed != mp {
		t.Fatalf("Remove returned (%v, %v); want (%p, true)", removed, ok, mp)
	}
	if r.Len() != 0 {
		t.Fatalf("after Remove len=%d, want 0", r.Len())
	}

	// Remove non-existent
	if _, ok := r.Remove("default:appA"); ok {
		t.Fatal("Remove of absent key returned ok=true, want false")
	}
}

func TestRegistryAddReplacesExisting(t *testing.T) {
	r := NewProcessRegistry()

	first := stub("first")
	second := stub("second")

	r.Add("default:appA", first)
	r.Add("default:appA", second) // should replace

	if r.Len() != 1 {
		t.Fatalf("len=%d, want 1 (replace should not add a new entry)", r.Len())
	}

	got, ok := r.Get("default:appA")
	if !ok || got != second {
		t.Fatalf("Get returned (%v, %v); want (%p, true)", got, ok, second)
	}
	if got == first {
		t.Fatal("expected second to replace first; both pointers returned")
	}
}

func TestRegistrySnapshotIsIndependent(t *testing.T) {
	r := NewProcessRegistry()
	r.Add("default:a", stub("a"))
	r.Add("default:b", stub("b"))

	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot len=%d, want 2", len(snap))
	}

	// Mutating the original mp's Info must NOT change the snapshotted
	// ProcessInfo value (since Snapshot copies by value).
	r.UpdateInfo("default:a", func(mp *ManagedProcess) {
		mp.Info.Status = process.StatusStopped
	})

	for _, info := range snap {
		if info.Status == process.StatusStopped {
			t.Fatal("Snapshot leaked live mutation; expected value-type copy")
		}
	}
}

// TestRegistrySnapshotOne covers the read-side counterpart to UpdateInfo:
// a value copy taken under the read lock. The copy must be independent of
// later live mutations (the whole point of routing naked reads through it
// rather than touching mp.Info.* directly), must report absence honestly,
// and must deep-copy the slice/map-backed AppConfig fields so a caller
// appending to the returned BaseEnv cannot corrupt the live entry.
func TestRegistrySnapshotOne(t *testing.T) {
	r := NewProcessRegistry()

	// Absent key → (zero, false).
	if info, ok := r.SnapshotOne("default:nope"); ok || info.PID != 0 {
		t.Fatalf("SnapshotOne on absent key returned (%+v, %v); want (zero, false)", info, ok)
	}

	mp := stub("deep")
	mp.Info.ID = 7
	mp.Info.PID = 1234
	mp.Info.Status = process.StatusOnline
	mp.Info.Restarts = 3
	mp.Info.Version = "1.2.3"
	mp.Info.BaseEnv = []string{"A=1", "B=2"}
	mp.Info.Env = map[string]string{"K": "V"}
	r.Add("default:deep", mp)

	info, ok := r.SnapshotOne("default:deep")
	if !ok {
		t.Fatal("SnapshotOne on existing key returned ok=false")
	}
	if info.ID != 7 || info.PID != 1234 || info.Status != process.StatusOnline ||
		info.Restarts != 3 || info.Version != "1.2.3" {
		t.Fatalf("SnapshotOne returned wrong values: %+v", info)
	}
	if len(info.BaseEnv) != 2 || info.BaseEnv[0] != "A=1" {
		t.Fatalf("BaseEnv not copied correctly: %+v", info.BaseEnv)
	}
	if info.Env["K"] != "V" {
		t.Fatalf("Env map not copied correctly: %+v", info.Env)
	}

	// 1. Snapshot is frozen against later live mutations: mutating the
	//    entry after the snapshot must NOT change the value we got back.
	r.UpdateInfo("default:deep", func(mp *ManagedProcess) {
		mp.Info.PID = 9999
		mp.Info.Status = process.StatusStopped
		mp.Info.Restarts = 42
	})
	if info.PID != 1234 || info.Status != process.StatusOnline || info.Restarts != 3 {
		t.Fatalf("SnapshotOne value leaked live mutation: PID=%d Status=%s Restarts=%d",
			info.PID, info.Status, info.Restarts)
	}

	// 2. Slice fields are deep-copied: appending to the returned BaseEnv
	//    must NOT mutate the live entry's BaseEnv.
	info.BaseEnv = append(info.BaseEnv, "C=3")
	live, _ := r.SnapshotOne("default:deep")
	if len(live.BaseEnv) != 2 {
		t.Fatalf("caller append to returned BaseEnv corrupted live entry: %+v", live.BaseEnv)
	}

	// 3. Concurrent reads (SnapshotOne) vs writes (UpdateInfo) stay
	//    race-clean — run under `go test -race` to verify.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_, _ = r.SnapshotOne("default:deep")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			r.UpdateInfo("default:deep", func(mp *ManagedProcess) {
				mp.Info.Restarts++
			})
		}
	}()
	wg.Wait()
}

func TestRegistryFindByTarget(t *testing.T) {
	r := NewProcessRegistry()

	// setup: 4 processes across 2 namespaces
	a := stub("appA")
	a.Info.ID = 0
	b1 := stub("appB")
	b1.Info.ID = 1
	b1.Info.Namespace = "Infra"
	c := stub("appC")
	c.Info.ID = 2
	c.Info.Namespace = "Infra"
	d := stub("appB")
	d.Info.ID = 3
	d.Info.Namespace = "default"

	r.Add("default:appA", a)
	r.Add("Infra:appB", b1)
	r.Add("Infra:appC", c)
	r.Add("default:appB", d)

	// 0. exact key match
	got := r.FindByTarget("Infra:appB")
	if len(got) != 1 || got[0] != b1 {
		t.Fatalf("exact-key match returned %v, want [%p]", got, b1)
	}

	// 1. ID match (1 → b1)
	got = r.FindByTarget("1")
	if len(got) != 1 || got[0] != b1 {
		t.Fatalf("ID '1' returned %v, want [%p]", got, b1)
	}

	// 2. name match (appB → both b1 and d, since same name in two namespaces)
	got = r.FindByTarget("appB")
	if len(got) != 2 {
		t.Fatalf("name 'appB' returned %d matches, want 2", len(got))
	}

	// 3. namespace match (Infra → b1 + c)
	got = r.FindByTarget("Infra")
	if len(got) != 2 {
		t.Fatalf("namespace 'Infra' returned %d matches, want 2", len(got))
	}

	// "all"
	got = r.FindByTarget("all")
	if len(got) != 4 {
		t.Fatalf("'all' returned %d matches, want 4", len(got))
	}
}

func TestRegistryUpdateInfo(t *testing.T) {
	r := NewProcessRegistry()
	r.Add("default:a", stub("a"))

	ok := r.UpdateInfo("default:a", func(mp *ManagedProcess) {
		mp.Info.Status = process.StatusStopped
		mp.Info.Restarts = 3
	})
	if !ok {
		t.Fatal("UpdateInfo on existing key returned false")
	}

	got, _ := r.Get("default:a")
	if got.Info.Status != process.StatusStopped {
		t.Fatalf("Status=%v, want StatusStopped", got.Info.Status)
	}
	if got.Info.Restarts != 3 {
		t.Fatalf("Restarts=%d, want 3", got.Info.Restarts)
	}

	// UpdateInfo on absent key
	if r.UpdateInfo("default:nope", func(*ManagedProcess) {}) {
		t.Fatal("UpdateInfo on absent key returned true; want false")
	}
}

func TestRegistryUpdateMetricsPIDCheck(t *testing.T) {
	r := NewProcessRegistry()
	mp := stub("a")
	mp.Info.PID = 100
	mp.Info.Status = process.StatusOnline
	r.Add("default:a", mp)

	// matching PID → write
	if !r.UpdateMetrics("default:a", 100, 42.5, 1024) {
		t.Fatal("UpdateMetrics with matching PID returned false")
	}
	got, _ := r.Get("default:a")
	if got.Info.CPU != 42.5 || got.Info.Memory != 1024 {
		t.Fatalf("got cpu=%f mem=%d, want 42.5/1024", got.Info.CPU, got.Info.Memory)
	}

	// stale PID → no write
	if r.UpdateMetrics("default:a", 999, 99.9, 9999) {
		t.Fatal("UpdateMetrics with stale PID returned true; want false")
	}
	got, _ = r.Get("default:a")
	if got.Info.CPU != 42.5 {
		t.Fatalf("stale write leaked; cpu=%f, want 42.5", got.Info.CPU)
	}

	// absent key → no write
	if r.UpdateMetrics("default:nope", 100, 1, 1) {
		t.Fatal("UpdateMetrics on absent key returned true; want false")
	}
}

func TestRegistryUpdateCronStatus(t *testing.T) {
	r := NewProcessRegistry()
	r.Add("default:a", stub("a"))

	now := time.Now()
	if !r.UpdateCronStatus("default:a", now, "ok") {
		t.Fatal("UpdateCronStatus on existing key returned false")
	}
	got, _ := r.Get("default:a")
	if !got.Info.LastCronAt.Equal(now) {
		t.Fatalf("LastCronAt=%v, want %v", got.Info.LastCronAt, now)
	}
	if got.Info.LastCronStatus != "ok" {
		t.Fatalf("LastCronStatus=%q, want 'ok'", got.Info.LastCronStatus)
	}
}

func TestRegistryConcurrentReadWrite(t *testing.T) {
	r := NewProcessRegistry()

	// pre-seed 100 processes
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("default:app%03d", i)
		r.Add(key, stub(fmt.Sprintf("app%03d", i)))
	}

	const goroutines = 50
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines * 3) // 3 categories of goroutine

	// writers — Add/Remove churn
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := fmt.Sprintf("default:churn-%d-%d", id, j)
				mp := stub("churn")
				r.Add(key, mp)
				r.Get(key)
				r.Remove(key)
			}
		}(i)
	}

	// readers — Get/List churn
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = r.List()
				_ = r.Snapshot()
				_ = r.Len()
			}
		}()
	}

	// updaters — UpdateInfo/UpdateMetrics churn on seeded keys
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := fmt.Sprintf("default:app%03d", (id*7+j)%100)
				r.UpdateInfo(key, func(mp *ManagedProcess) {
					mp.Info.Restarts++
				})
				// Re-fetch to learn the live PID (mp from the closure
				// above is out of scope here). We pass it back into
				// UpdateMetrics so the PID-check has a chance to pass.
				if mp, ok := r.Get(key); ok {
					r.UpdateMetrics(key, mp.Info.PID, float64(j%100), uint64(j*1024))
				}
			}
		}(i)
	}

	wg.Wait()

	// Final state: 100 seeded apps remain, all churn entries cleaned up.
	if r.Len() != 100 {
		t.Fatalf("final Len=%d, want 100", r.Len())
	}
}

func TestRegistryEscapeHatchLocks(t *testing.T) {
	r := NewProcessRegistry()

	// 1. Recursive RLock from same goroutine is permitted by Go ≥1.18 —
	//    r.Get() takes its own RLock internally. The escape-hatch
	//    pattern (external RLock + Get + external RUnlock) is therefore
	//    safe but redundant; the high-level Get alone would suffice.
	r.RLock()
	_, ok := r.Get("default:nope")
	r.RUnlock()
	if ok {
		t.Fatal("Get on absent returned ok=true under RLock")
	}

	// 2. The escape hatches Lock/Unlock/RLock/RUnlock exist for callers
	//    that need to do a multi-step direct mutation of the underlying
	//    map under a held lock. They MUST NOT be used to chain other
	//    ProcessRegistry methods (sync.RWMutex forbids recursive Lock,
	//    and recursive RLock only works in the no-pending-writer case
	//    which is not guaranteed across calls).
	//
	//    We exercise the escape hatches here purely to confirm
	//    acquire/release round-trip works and a non-recursive call from
	//    a fresh goroutine is unblocked once Unlock fires.
	r.Lock()
	r.Unlock() // bare acquire+release

	r.RLock()
	r.RUnlock()

	// 3. Confirm that after a clean release, normal methods work.
	r.Add("default:after-escape", stub("after-escape"))
	got, ok := r.Get("default:after-escape")
	if !ok || got.Info.Name != "after-escape" {
		t.Fatalf("post-escape Get returned (%v, %v)", got, ok)
	}
}