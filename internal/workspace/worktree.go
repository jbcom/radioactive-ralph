package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// Worktree describes a live git worktree created by the manager.
type Worktree struct {
	// Slot is the worktree index inside the pool (0..MaxParallelWorktrees-1).
	Slot int

	// Path is the absolute path on disk where the worktree is checked out.
	Path string

	// Branch is the ref HEAD points to inside the worktree.
	Branch string
}

// worktreeState tracks the in-memory pool.
type worktreeState struct {
	mu        sync.Mutex
	inFlight  map[int]*Worktree // slot → worktree
	generator int               // monotonic counter for branch naming
}

var errPoolFull = errors.New("workspace: worktree pool full")

// Pool returns the current list of live worktrees. Callers should not
// mutate the slice.
func (m *Manager) Pool() []*Worktree {
	m.ensureState()
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	out := make([]*Worktree, 0, len(m.state.inFlight))
	for _, w := range m.state.inFlight {
		out = append(out, w)
	}
	return out
}

// AcquireWorktree creates a new worktree for an available slot and
// returns it. Returns errPoolFull if all slots are in use.
//
// Branch is named `ralph/<variant>/<slot>-<gen>` where gen is a
// monotonic counter so recycled slots never collide with stale
// worktree admin data in git.
func (m *Manager) AcquireWorktree(ctx context.Context) (*Worktree, error) {
	if m.Isolation == variant.IsolationShared {
		return nil, errors.New("workspace(shared): no worktrees")
	}
	if m.Isolation == variant.IsolationShallow {
		// Shallow isolation uses a single checkout; treat it as one
		// reusable worktree.
		return &Worktree{
			Slot:   0,
			Path:   m.Paths.Shallow,
			Branch: "HEAD",
		}, nil
	}

	m.ensureState()
	m.state.mu.Lock()
	slot := -1
	limit := m.Variant.MaxParallelWorktrees
	for i := 0; i < limit; i++ {
		if _, taken := m.state.inFlight[i]; !taken {
			slot = i
			break
		}
	}
	if slot < 0 {
		m.state.mu.Unlock()
		return nil, errPoolFull
	}
	m.state.generator++
	gen := m.state.generator
	// Reserve the slot atomically — the Unlock is below. We take a
	// placeholder so a concurrent AcquireWorktree cannot double-book.
	m.state.inFlight[slot] = &Worktree{Slot: slot}
	m.state.mu.Unlock()

	branch := fmt.Sprintf("ralph/%s/%d-%d", m.Variant.Name, slot, gen)
	wtPath := filepath.Join(m.Paths.Worktrees, fmt.Sprintf("slot-%d", slot))

	// Clean any stale dir from a prior crashed run before re-creating.
	if _, err := os.Stat(wtPath); err == nil {
		if err := os.RemoveAll(wtPath); err != nil {
			m.releaseSlot(slot)
			return nil, fmt.Errorf("remove stale worktree %s: %w", wtPath, err)
		}
	}

	if err := runGit(ctx, m.Paths.MirrorGit,
		"worktree", "add", "-b", branch, wtPath, "HEAD"); err != nil {
		m.releaseSlot(slot)
		// `worktree add` may have left admin state; best-effort prune.
		_ = runGit(ctx, m.Paths.MirrorGit, "worktree", "prune")
		return nil, fmt.Errorf("worktree add: %w", err)
	}

	wt := &Worktree{Slot: slot, Path: wtPath, Branch: branch}
	m.state.mu.Lock()
	m.state.inFlight[slot] = wt
	m.state.mu.Unlock()
	return wt, nil
}

// ReleaseWorktree removes a worktree and frees its slot. Safe to call
// on a crashed worktree — uses `worktree remove --force`.
func (m *Manager) ReleaseWorktree(ctx context.Context, wt *Worktree) error {
	if wt == nil {
		return nil
	}
	if m.Isolation == variant.IsolationShallow {
		return nil // shared checkout; nothing to release
	}
	m.ensureState()

	err := runGit(ctx, m.Paths.MirrorGit, "worktree", "remove", "--force", wt.Path)
	if err != nil {
		// If remove fails (e.g., path missing), prune the admin state
		// manually so the slot can be reused.
		_ = runGit(ctx, m.Paths.MirrorGit, "worktree", "prune")
	}
	m.releaseSlot(wt.Slot)
	return err
}

// Reconcile walks `git worktree list` and removes any registered
// worktrees whose disk paths have disappeared (likely from an
// ungraceful prior shutdown). Called at supervisor boot after replaying
// the event log.
func (m *Manager) Reconcile(ctx context.Context) error {
	if m.Isolation == variant.IsolationShared || m.Isolation == variant.IsolationShallow {
		return nil
	}
	out, err := gitOutput(ctx, m.Paths.MirrorGit, "worktree", "list", "--porcelain")
	if err != nil {
		return fmt.Errorf("worktree list: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		path := strings.TrimPrefix(line, "worktree ")
		// Skip the bare mirror itself.
		if path == m.Paths.MirrorGit || strings.TrimSuffix(path, "/") == m.Paths.MirrorGit {
			continue
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// Disk gone — prune admin state for this worktree.
			_ = runGit(ctx, m.Paths.MirrorGit, "worktree", "prune")
			break // one prune handles all missing worktrees
		}
	}
	return nil
}

// ensureState lazy-initializes the worktree pool map.
func (m *Manager) ensureState() {
	m.stateOnce.Do(func() {
		m.state = &worktreeState{inFlight: make(map[int]*Worktree)}
	})
}

// releaseSlot frees slot under the state lock.
func (m *Manager) releaseSlot(slot int) {
	m.state.mu.Lock()
	delete(m.state.inFlight, slot)
	m.state.mu.Unlock()
}
