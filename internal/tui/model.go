package tui

import (
	"context"
	"encoding/json"
	"maps"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// level is the current drill-down depth (spec §7: macro -> meso -> micro).
type level int

const (
	levelMacro level = iota
	levelMeso
	levelMicro
)

// snapshot is everything the current view needs, re-fetched wholesale on
// every refresh tick and on every drill transition. Keeping it one struct
// (rather than each view owning private mutable state) makes Update's
// transitions easy to reason about and to unit test: a key message either
// changes level/cursor or it doesn't, and the snapshot is always a
// straight read from DataSource.
type snapshot struct {
	status ipc.StatusReply

	plans     []store.Plan
	progress  map[string]orch.Progress // planID -> progress
	planEvent []store.Event            // recent project-wide events (macro view)

	tasks     []store.Task  // meso: tasks for the selected plan
	taskEvent []store.Event // micro: recent events for the selected task
	live      []liveLogLine // micro: frames observed via Attach, newest last
}

// liveLogLine is one line rendered in the micro view's scrolling tail. It
// may originate from a stored task event (on drill-in) or a live Attach
// frame (appended as the stream runs).
type liveLogLine struct {
	at   time.Time
	text string
}

// Model is the root tea.Model. It holds the current drill level, the
// read-only DataSource, and the last-fetched snapshot; Update handles key
// events and the periodic refresh tick, View delegates to the per-level
// renderer. Model never calls anything on DataSource except its documented
// read methods — see datasource.go's DataSource doc comment for the
// read-only enforcement point.
type Model struct {
	ctx    context.Context
	source DataSource

	projectID string

	lvl  level
	snap snapshot

	// cursor tracks the selected row within the CURRENT level's list
	// (plans at macro, tasks at meso; meaningless/unused at micro).
	cursor int

	// selectedPlan/selectedTask carry the drill-in choice down to meso/
	// micro so a refresh at those levels knows what to re-fetch.
	selectedPlan store.Plan
	selectedTask store.Task

	viewport viewportState // micro: scroll offset into the log tail

	width, height int

	err error

	// attachCancel stops the current level's live Attach subscription
	// (micro only) when drilling out or quitting. Nil when no
	// subscription is active.
	attachCancel context.CancelFunc

	// attachFrames/attachDone are the current subscription's channels, held
	// so the liveFrameMsg handler can RE-ISSUE attachCmd after every frame —
	// Bubble Tea models a stream as a command that must be re-armed each
	// delivery. Without re-arming, the stream stopped after one frame and the
	// forwarder goroutine leaked (blocked writing to a channel no one read).
	attachFrames chan json.RawMessage
	attachDone   chan error
	// attachEpoch increments on every new subscription; a liveFrameMsg
	// carrying a stale epoch (from a subscription the user already drilled out
	// of) is dropped rather than re-arming the current one.
	attachEpoch uint64

	// fetching is true while a refresh gather is in flight. The 1s refresh
	// tick fires unconditionally, so without this guard a gather that outlives
	// its interval (large plan set, contended SQLite, slow supervisor) would
	// have the next tick dispatch a SECOND overlapping gather — stacking
	// supervisor connections and letting an older gather's result land after a
	// newer one. The tick skips fetchCmd while a gather is outstanding.
	fetching bool

	quitting bool
}

// viewportState is the micro view's scroll position.
type viewportState struct {
	offset int
}

// NewModel constructs the root model. ctx bounds the whole TUI session —
// cancelling it (e.g. on SIGINT) unwinds any in-flight Attach goroutine.
func NewModel(ctx context.Context, source DataSource, projectID string) Model {
	return Model{
		ctx:       ctx,
		source:    source,
		projectID: projectID,
		snap: snapshot{
			progress: map[string]orch.Progress{},
		},
	}
}

// Init starts the refresh loop. It fires an IMMEDIATE refresh tick rather than
// launching a fetch directly, so the very first gather goes through the same
// in-flight-guarded path as every periodic tick (Init returns a Cmd and cannot
// set m.fetching, so a direct fetch here could overlap the first periodic tick
// if the initial gather is slow).
func (m Model) Init() tea.Cmd {
	return immediateTickCmd()
}

// immediateTickCmd fires a refreshMsg with no delay, to prime the refresh loop.
func immediateTickCmd() tea.Cmd {
	return func() tea.Msg { return refreshMsg(time.Time{}) }
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return refreshMsg(t)
	})
}

// fetchedMsg carries the result of a (re)fetch back into Update.
type fetchedMsg struct {
	snap snapshot
	err  error
}

// liveFrameMsg carries one Attach event frame back into Update. Only
// produced while in levelMicro with a live subscription active. epoch tags
// the subscription that produced it, so a late frame from a subscription the
// user already drilled out of is ignored instead of re-arming (which would
// start a duplicate loop on the NEW subscription's channels).
type liveFrameMsg struct {
	raw   json.RawMessage
	epoch uint64
}

// attachEndedMsg signals the Attach stream ended (cleanly or with error).
// epoch tags the subscription it belongs to, so a stale end-message from a
// subscription the user already drilled out of doesn't clear the CURRENT
// subscription's channels (which would silently stop the new stream).
type attachEndedMsg struct {
	err   error
	epoch uint64
}

// startFetch marks a gather in flight and returns its command. EVERY path that
// launches a gather (periodic refresh, drill-in, drill-out) must go through this
// so the in-flight guard tracks all of them — otherwise an untracked gather
// overlaps the periodic one and its fetchedMsg clears the shared flag out from
// under the tracked gather. Caller must have already decided a gather is wanted
// (e.g. the refresh path skips when m.fetching is already set).
func (m *Model) startFetch() tea.Cmd {
	m.fetching = true
	return m.fetchCmd()
}

// fetchCmd re-fetches everything the current level needs.
func (m Model) fetchCmd() tea.Cmd {
	lvl := m.lvl
	source := m.source
	ctx := m.ctx
	projectID := m.projectID
	selectedPlan := m.selectedPlan
	selectedTask := m.selectedTask
	prevProgress := m.snap.progress

	return func() tea.Msg {
		// Bound the whole gather so a slow/hung supervisor degrades to an error
		// (surfaced in the header) instead of blocking forever — which, with the
		// in-flight guard, would otherwise stall all future refreshes. A few
		// refresh intervals is plenty of headroom for a healthy round trip.
		ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()

		status, err := source.Status(ctx)
		if err != nil {
			return fetchedMsg{err: err}
		}

		snap := snapshot{status: status, progress: map[string]orch.Progress{}}
		maps.Copy(snap.progress, prevProgress)

		switch lvl {
		case levelMacro:
			plans, err := source.ListPlans(ctx, projectID)
			if err != nil {
				return fetchedMsg{err: err}
			}
			snap.plans = plans
			for _, p := range plans {
				if prog, err := source.PlanProgress(ctx, p.ID); err == nil {
					snap.progress[p.ID] = prog
				}
			}
			events, err := source.ListProjectEvents(ctx, projectID, 10)
			if err != nil {
				return fetchedMsg{err: err}
			}
			snap.planEvent = events

		case levelMeso:
			tasks, err := source.ListTasks(ctx, selectedPlan.ID)
			if err != nil {
				return fetchedMsg{err: err}
			}
			snap.tasks = tasks
			if prog, err := source.PlanProgress(ctx, selectedPlan.ID); err == nil {
				snap.progress[selectedPlan.ID] = prog
			}

		case levelMicro:
			events, err := source.ListTaskEvents(ctx, selectedPlan.ID, selectedTask.ID, 50)
			if err != nil {
				return fetchedMsg{err: err}
			}
			snap.taskEvent = events
		}

		return fetchedMsg{snap: snap}
	}
}

// attachCmd starts (or continues) the live Attach subscription for the
// micro view. It runs on its own goroutine via tea's command mechanism and
// feeds frames back as liveFrameMsg; Update re-issues attachCmd after each
// frame is delivered so the subscription keeps flowing (Bubble Tea's
// convention for representing a channel/stream as commands).
func attachCmd(ctx context.Context, frames chan json.RawMessage, done chan error, epoch uint64) tea.Cmd {
	return func() tea.Msg {
		select {
		case raw, ok := <-frames:
			if !ok {
				return attachEndedMsg{err: <-done, epoch: epoch}
			}
			return liveFrameMsg{raw: raw, epoch: epoch}
		case <-ctx.Done():
			return attachEndedMsg{err: ctx.Err(), epoch: epoch}
		}
	}
}

// startAttach launches source.Attach on a background goroutine that
// forwards frames onto a channel, and returns the channels plus a cancel
// func the model uses to stop it on drill-out. This keeps the actual
// blocking Attach call off Bubble Tea's Update goroutine.
func startAttach(parent context.Context, source DataSource) (frames chan json.RawMessage, done chan error, cancel context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	frames = make(chan json.RawMessage, 32)
	done = make(chan error, 1)
	go func() {
		err := source.Attach(ctx, func(raw json.RawMessage) error {
			select {
			case frames <- raw:
			case <-ctx.Done():
			}
			return nil
		})
		close(frames)
		done <- err
	}()
	return frames, done, cancel
}

// Update handles key events (arrows/enter to drill in, esc/backspace to
// drill out, q to quit) and the periodic refresh tick. This is the surface
// the model_test.go table tests exercise directly, injecting tea.KeyMsg
// values without a real terminal.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case refreshMsg:
		// Always keep the tick going, but only start a new gather when the prior
		// one has returned — otherwise a slow gather lets ticks stack overlapping
		// fetches (see Model.fetching).
		if m.fetching {
			return m, tickCmd()
		}
		return m, tea.Batch(m.startFetch(), tickCmd())

	case fetchedMsg:
		m.fetching = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		// Capture the identity of the currently-selected row BEFORE the merge
		// replaces the list, so we can re-find it afterwards (a refresh that
		// removes/reorders rows must keep the SAME entity selected, not just an
		// in-bounds index — see reconcileCursor).
		selectedID := m.selectedRowID()
		// Merge rather than replace: a fetch for one level should not
		// clobber fields owned by another level (e.g. a macro refresh
		// while the operator is mid-drill should not blank meso/micro
		// data — but in practice fetchCmd only runs for the CURRENT
		// level, so this mostly just carries status/progress forward).
		m.snap.status = msg.snap.status
		if msg.snap.plans != nil {
			m.snap.plans = msg.snap.plans
		}
		if msg.snap.progress != nil {
			m.snap.progress = msg.snap.progress
		}
		if msg.snap.planEvent != nil {
			m.snap.planEvent = msg.snap.planEvent
		}
		if msg.snap.tasks != nil {
			m.snap.tasks = msg.snap.tasks
		}
		if msg.snap.taskEvent != nil {
			m.snap.taskEvent = msg.snap.taskEvent
		}
		// Re-point the cursor at the SAME entity it was on before the refresh
		// (by ID), falling back to a clamp if that entity is gone. Without this,
		// a refresh that removes/reorders a row ahead of the cursor would leave
		// the (still in-bounds) index selecting a DIFFERENT entity than the
		// operator saw — so drilling in would open the wrong plan/task.
		m.reconcileCursor(selectedID)
		return m, nil

	case liveFrameMsg:
		// Drop a late frame from a subscription the user already drilled out
		// of (its epoch no longer matches): appending it would pollute the new
		// view, and re-arming on it would start a duplicate loop on the
		// current subscription's channels.
		if msg.epoch != m.attachEpoch {
			return m, nil
		}
		m.snap.live = append(m.snap.live, liveLogLine{at: time.Now(), text: renderFrame(msg.raw)})
		if len(m.snap.live) > 500 {
			m.snap.live = m.snap.live[len(m.snap.live)-500:]
		}
		// Re-arm the stream: pull the NEXT frame. Without this the
		// subscription delivers exactly one frame and the forwarder goroutine
		// blocks forever on the unread channel.
		if m.attachFrames != nil {
			return m, attachCmd(m.ctx, m.attachFrames, m.attachDone, m.attachEpoch)
		}
		return m, nil

	case attachEndedMsg:
		// Ignore a stale end from a subscription the user already drilled out
		// of — clearing the channels here would kill the CURRENT subscription.
		if msg.epoch != m.attachEpoch {
			return m, nil
		}
		// The current stream closed (clean end, error, or ctx cancel) — stop
		// pulling and drop the channel references so a later re-arm can't
		// reuse them.
		m.attachFrames = nil
		m.attachDone = nil
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		if m.attachCancel != nil {
			m.attachCancel()
		}
		return m, tea.Quit

	case "up", "k":
		if m.lvl == levelMicro {
			m.viewport.offset++
			return m, nil
		}
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case "down", "j":
		if m.lvl == levelMicro {
			if m.viewport.offset > 0 {
				m.viewport.offset--
			}
			return m, nil
		}
		if m.cursor < m.currentListLen()-1 {
			m.cursor++
		}
		return m, nil

	case "enter", "right", "l":
		return m.drillIn()

	case "esc", "backspace", "left", "h":
		return m.drillOut()
	}
	return m, nil
}

// currentListLen is the length of the selectable list at the current
// level, used to bound cursor movement.
func (m Model) currentListLen() int {
	switch m.lvl {
	case levelMacro:
		return len(m.snap.plans)
	case levelMeso:
		return len(m.snap.tasks)
	default:
		return 0
	}
}

// selectableIDs returns the stable IDs of the current level's rows, in the
// SAME order the cursor walks and the view renders (macro: plans as-is; meso:
// tasks in flattened grouped order). Returns nil at micro (no row selection).
func (m Model) selectableIDs() []string {
	switch m.lvl {
	case levelMacro:
		ids := make([]string, len(m.snap.plans))
		for i, p := range m.snap.plans {
			ids[i] = p.ID
		}
		return ids
	case levelMeso:
		flat := flattenGroupedTasks(m.snap.tasks)
		ids := make([]string, len(flat))
		for i, t := range flat {
			ids[i] = t.ID
		}
		return ids
	default:
		return nil
	}
}

// selectedRowID is the ID of the row the cursor currently points at, or ""
// when there is no such row (empty list / out of range / micro level).
func (m Model) selectedRowID() string {
	ids := m.selectableIDs()
	if m.cursor < 0 || m.cursor >= len(ids) {
		return ""
	}
	return ids[m.cursor]
}

// reconcileCursor re-points the cursor at wantID in the (possibly refreshed)
// current list, preserving the SELECTED ENTITY across a refresh that removed or
// reordered rows — not merely a numeric index. If wantID is gone (or was empty),
// it clamps the existing index in-bounds so the highlight stays visible. An
// empty list parks the cursor at 0.
func (m *Model) reconcileCursor(wantID string) {
	ids := m.selectableIDs()
	if len(ids) == 0 {
		m.cursor = 0
		return
	}
	if wantID != "" {
		for i, id := range ids {
			if id == wantID {
				m.cursor = i
				return
			}
		}
	}
	// Entity gone (or none was selected): clamp the index in-bounds.
	if m.cursor >= len(ids) {
		m.cursor = len(ids) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// drillIn moves macro->meso->micro, recording the selected row so the
// next level's fetch knows what to load. Drilling into micro starts the
// live Attach subscription.
func (m Model) drillIn() (tea.Model, tea.Cmd) {
	switch m.lvl {
	case levelMacro:
		if m.cursor >= len(m.snap.plans) {
			return m, nil
		}
		m.selectedPlan = m.snap.plans[m.cursor]
		m.lvl = levelMeso
		m.cursor = 0
		return m, m.drillFetch()

	case levelMeso:
		// Select from the SAME grouped order the meso view renders and the
		// cursor walks — not the raw m.snap.tasks order — so the highlighted
		// row and the drilled-into task are always the same one.
		flat := flattenGroupedTasks(m.snap.tasks)
		if m.cursor >= len(flat) {
			return m, nil
		}
		m.selectedTask = flat[m.cursor]
		m.lvl = levelMicro
		m.snap.live = nil
		m.viewport = viewportState{}
		frames, done, cancel := startAttach(m.ctx, m.source)
		m.attachCancel = cancel
		m.attachFrames = frames
		m.attachDone = done
		m.attachEpoch++
		return m, tea.Batch(m.drillFetch(), attachCmd(m.ctx, frames, done, m.attachEpoch))

	default:
		return m, nil
	}
}

// drillOut moves micro->meso->macro. Leaving micro stops the live Attach
// subscription.
func (m Model) drillOut() (tea.Model, tea.Cmd) {
	switch m.lvl {
	case levelMicro:
		if m.attachCancel != nil {
			m.attachCancel()
			m.attachCancel = nil
		}
		m.lvl = levelMeso
		m.cursor = 0
		return m, m.drillFetch()

	case levelMeso:
		m.lvl = levelMacro
		m.cursor = 0
		return m, m.drillFetch()

	default:
		return m, nil
	}
}

// drillFetch launches the new level's gather after a drill, respecting the
// in-flight guard: if a gather is already outstanding it fires nothing (the
// navigation already took effect via the level/cursor change, and the next
// periodic tick fetches the new level's data once the outstanding gather
// clears). This keeps every gather tracked by m.fetching so drills can't stack
// an overlapping fetch on top of a slow periodic one.
func (m *Model) drillFetch() tea.Cmd {
	if m.fetching {
		return nil
	}
	return m.startFetch()
}

// View delegates to the level renderer.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	switch m.lvl {
	case levelMacro:
		return renderMacro(m)
	case levelMeso:
		return renderMeso(m)
	case levelMicro:
		return renderMicro(m)
	default:
		return ""
	}
}

// renderFrame renders one raw Attach event frame as a single human-
// readable log line. Best-effort: an event whose shape doesn't match the
// expected {kind, task_id, ...} form still renders as raw JSON rather than
// being dropped, so nothing observed over the wire silently disappears
// from the pane.
func renderFrame(raw json.RawMessage) string {
	var probe struct {
		Kind   string `json:"kind"`
		TaskID string `json:"task_id"`
		Actor  string `json:"actor"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.Kind == "" {
		return string(raw)
	}
	if probe.TaskID != "" {
		return probe.Kind + " task=" + probe.TaskID + " actor=" + probe.Actor
	}
	return probe.Kind + " actor=" + probe.Actor
}
