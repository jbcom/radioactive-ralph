package tui

import (
	"context"
	"encoding/json"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
)

const (
	refreshInterval = time.Second
	fetchTimeout    = 5 * time.Second
)

type refreshMsg time.Time

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
	capturedAt time.Time
	summary    observe.Summary

	plans        []observe.Plan
	plansHasMore bool
	progress     map[string]progress
	planEvent    []observe.Event

	tasks        []observe.Task
	tasksHasMore bool
	// descriptions maps task id -> author-written label, fetched separately
	// from the content-safe snapshot via the opt-in TaskDetail query. Held
	// beside observe.Task rather than on it so the bulk DTO stays content-free.
	descriptions  map[string]string
	taskEvent     []observe.Event
	eventsHasMore bool
	eventCursor   int64
	live          []liveLogLine
}

type progress struct {
	Done  int
	Total int
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
	selectedPlan observe.Plan
	selectedTask observe.Task

	viewport viewportState // micro: scroll offset into the log tail

	width, height int

	err error

	// attachCancel stops the session-long live Attach subscription, called on
	// quit (the subscription runs for the whole session across all drill levels,
	// not per-micro-drill). Nil until the first fetch starts it (see
	// ensureAttach).
	attachCancel context.CancelFunc

	// attachFrames/attachDone are the current subscription's channels, held
	// so the liveFrameMsg handler can RE-ISSUE attachCmd after every frame —
	// Bubble Tea models a stream as a command that must be re-armed each
	// delivery. Without re-arming, the stream stopped after one frame and the
	// forwarder goroutine leaked (blocked writing to a channel no one read).
	attachFrames chan ipc.AttachEvent
	attachDone   chan error
	// attachEpoch increments on every new subscription; a liveFrameMsg
	// carrying a stale epoch (from a subscription the user already drilled out
	// of) is dropped rather than re-arming the current one.
	attachEpoch uint64

	// lastEventID is the model-owned resume cursor: the highest event id known
	// to have been seen. It is seeded once from the safe snapshot's project-wide
	// cursor before the first attach (attachSeeded), then advanced as live frames
	// arrive. ensureAttach
	// always passes it to the subscription, so a reconnect resumes from it and
	// gap events aren't missed — even if the first subscription ended before
	// yielding a frame (the model, not the datasource, owns the cursor).
	lastEventID int64
	// attachSeeded is set once lastEventID has been seeded from the snapshot's
	// project-wide event cursor, so
	// the seed happens exactly once (the first ensureAttach), not on every
	// reconnect (which must keep the advanced cursor, not reset it).
	attachSeeded bool

	// liveDoneTasks is the set of task-ids already counted as done by a LIVE
	// progress bump since the last poll reconcile. A single completion emits TWO
	// done-kind events (worker.completed from MarkDone, then worker.verified_done
	// from VerifyAndComplete), and at macro level snap.tasks is empty so the
	// per-task wasDone check can't dedup them — this set does, keyed by task-id
	// regardless of drill level. Cleared on each fetchedMsg (the poll's
	// safe plan progress is fresh truth and already counts every persisted done).
	liveDoneTasks map[string]bool

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
			progress: map[string]progress{},
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

// liveFrameMsg carries one Attach event frame back into Update. Produced for
// the whole session by the session-long subscription (all drill levels consume
// it). epoch tags the subscription that produced it, so a late frame from a
// prior subscription (e.g. after a reconnect bumped the epoch) is ignored
// instead of re-arming the current one's channels.
type liveFrameMsg struct {
	event ipc.AttachEvent
	raw   json.RawMessage // test/rolling decode seam; production sets event
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

// ensureAttach starts the session-long live event subscription if one isn't
// already running, and returns its command (or nil if already active). The
// subscription is project-scoped and runs for the whole TUI session — macro,
// meso, and micro all consume from it (routed by level in the liveFrameMsg
// handler), so the macro plan-overview and meso task-list update from events as
// they land, not just on the 1s poll. It is started lazily on the first
// completed fetch rather than in Init because Init returns a Cmd and cannot hold
// the subscription's channels on the model.
func (m *Model) ensureAttach() tea.Cmd {
	if m.attachFrames != nil {
		return nil
	}
	// Seed the model-owned cursor ONCE from the same safe supervisor snapshot
	// that produced the initial visible state. There is no separate raw-store
	// read and no error-to-zero fallback.
	if !m.attachSeeded {
		m.lastEventID = m.snap.eventCursor
		m.attachSeeded = true
	}
	// Always resume from the model's cursor: on the first attach it's the seeded
	// max ("from now"); on a reconnect it's the last id processed, so gap events
	// aren't missed.
	frames, done, cancel := startAttach(m.ctx, m.source, m.lastEventID)
	m.attachCancel = cancel
	m.attachFrames = frames
	m.attachDone = done
	m.attachEpoch++
	return attachCmd(m.ctx, frames, done, m.attachEpoch)
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

		query := observe.SnapshotQuery{
			ProjectID:  projectID,
			PlanLimit:  observe.MaxPageLimit,
			TaskLimit:  1,
			EventLimit: 1,
		}

		switch lvl {
		case levelMacro:
			query.EventLimit = 10

		case levelMeso:
			query.PlanID = selectedPlan.ID
			query.PlanLimit = 1
			query.TaskLimit = observe.MaxPageLimit

		case levelMicro:
			query.PlanID = selectedPlan.ID
			query.TaskID = selectedTask.ID
			query.PlanLimit = 1
			query.TaskLimit = 1
			query.EventLimit = 50
		}

		reply, err := source.Snapshot(ctx, query)
		if err != nil {
			return fetchedMsg{err: err}
		}
		snap := snapshot{
			capturedAt:  reply.CapturedAt,
			summary:     reply.Summary,
			eventCursor: reply.EventCursor,
			progress:    make(map[string]progress, len(prevProgress)+len(reply.Plans.Items)),
		}
		for id, value := range prevProgress {
			snap.progress[id] = value
		}
		for _, plan := range reply.Plans.Items {
			snap.progress[plan.ID] = progress{
				Done:  plan.TaskDone,
				Total: plan.TaskTotal,
			}
		}
		switch lvl {
		case levelMacro:
			snap.plans = reply.Plans.Items
			snap.plansHasMore = reply.Plans.HasMore
			snap.planEvent = reply.RecentEvents.Items
			snap.eventsHasMore = reply.RecentEvents.HasMore
		case levelMeso:
			snap.tasks = reply.Tasks.Items
			snap.tasksHasMore = reply.Tasks.HasMore
			snap.descriptions = fetchDescriptions(ctx, source, projectID, selectedPlan.ID, taskIDsOf(reply.Tasks.Items))
		case levelMicro:
			snap.taskEvent = reply.RecentEvents.Items
			snap.eventsHasMore = reply.RecentEvents.HasMore
			snap.descriptions = fetchDescriptions(
				ctx, source, projectID, selectedPlan.ID, []string{selectedTask.ID})
		}
		return fetchedMsg{snap: snap}
	}
}

// attachCmd starts (or continues) the live Attach subscription for the
// micro view. It runs on its own goroutine via tea's command mechanism and
// feeds frames back as liveFrameMsg; Update re-issues attachCmd after each
// frame is delivered so the subscription keeps flowing (Bubble Tea's
// convention for representing a channel/stream as commands).
func attachCmd(
	ctx context.Context,
	frames chan ipc.AttachEvent,
	done chan error,
	epoch uint64,
) tea.Cmd {
	return func() tea.Msg {
		select {
		case event, ok := <-frames:
			if !ok {
				return attachEndedMsg{err: <-done, epoch: epoch}
			}
			return liveFrameMsg{event: event, epoch: epoch}
		case <-ctx.Done():
			return attachEndedMsg{err: ctx.Err(), epoch: epoch}
		}
	}
}

// startAttach launches source.Attach on a background goroutine that
// forwards frames onto a channel, and returns the channels plus a cancel
// func the model uses to stop it. afterID is the resume cursor: >0 resumes
// from a known id (a reconnect passes the last id it processed), while zero
// starts from the beginning. The model normally supplies the safe snapshot's
// project-wide cursor for the initial attach. This keeps the actual blocking
// Attach call off Bubble Tea's Update goroutine.
func startAttach(
	parent context.Context,
	source DataSource,
	afterID int64,
) (
	frames chan ipc.AttachEvent,
	done chan error,
	cancel context.CancelFunc,
) {
	ctx, cancel := context.WithCancel(parent)
	frames = make(chan ipc.AttachEvent, 32)
	done = make(chan error, 1)
	go func() {
		err := source.Attach(ctx, afterID, func(event ipc.AttachEvent) error {
			select {
			case frames <- event:
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
		return m.handleFetched(msg)

	case liveFrameMsg:
		// Drop a late frame from a subscription the user already drilled out
		// of (its epoch no longer matches): appending it would pollute the new
		// view, and re-arming on it would start a duplicate loop on the
		// current subscription's channels.
		if msg.epoch != m.attachEpoch {
			return m, nil
		}
		rearm := func() (tea.Model, tea.Cmd) {
			// Re-arm the stream: pull the NEXT frame. Without this the
			// subscription delivers exactly one frame and the forwarder
			// goroutine blocks forever on the unread channel.
			if m.attachFrames != nil {
				return m, attachCmd(m.ctx, m.attachFrames, m.attachDone, m.attachEpoch)
			}
			return m, nil
		}

		ev := msg.event
		if ev.Kind == "" && len(msg.raw) > 0 {
			var ok bool
			ev, ok = decodeEvent(msg.raw)
			if !ok {
				return rearm()
			}
		}

		// Advance the resume cursor: on a later reconnect, ensureAttach resumes
		// from the highest id processed so gap events aren't missed. Events stream
		// in ascending id order, but guard with a max in case of any reordering.
		if ev.ID > m.lastEventID {
			m.lastEventID = ev.ID
		}

		// Apply the event as a live delta to the macro/meso snapshot so a
		// task/plan lifecycle change lands immediately instead of only on the
		// next poll. The periodic poll still runs every tick and remains the
		// source of truth that reconciles any missed or duplicated frame.
		m.snap = applyEvent(m.snap, ev)
		// And advance the macro plan-PROGRESS counter for a fresh completion
		// (dedup + monotonic — see bumpLiveProgress).
		m.bumpLiveProgress(ev)

		// Macro event pane: prepend to the live project-event tail (newest-first,
		// like the poll refill), so the macro overview is a live feed, not a 1s
		// snapshot. De-dupe by id so a poll landing right after a live prepend
		// doesn't show the same event twice.
		m.snap.planEvent = prependEvent(m.snap.planEvent, ev)

		// Micro-view per-task log: only append frames relevant to the SELECTED
		// task, so drilling into one task doesn't fill its one-worker log with
		// unrelated tasks' activity. A task-agnostic frame (no task_id — plan/
		// service level) is still shown as context.
		if m.lvl == levelMicro {
			if ev.TaskID == "" || m.selectedTask.ID == "" || ev.TaskID == m.selectedTask.ID {
				at := ev.OccurredAt
				if at.IsZero() {
					at = time.Now()
				}
				m.snap.live = append(m.snap.live, liveLogLine{at: at, text: renderEvent(ev)})
				if len(m.snap.live) > 500 {
					m.snap.live = m.snap.live[len(m.snap.live)-500:]
				}
			}
		}
		return rearm()

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

func (m Model) handleFetched(msg fetchedMsg) (tea.Model, tea.Cmd) {
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
	m.snap.capturedAt = msg.snap.capturedAt
	m.snap.summary = msg.snap.summary
	m.snap.eventCursor = msg.snap.eventCursor
	if msg.snap.plans != nil {
		m.snap.plans = msg.snap.plans
		m.snap.plansHasMore = msg.snap.plansHasMore
	}
	if msg.snap.progress != nil {
		// Reconcile progress toward the poll WITHOUT regressing a live bump a
		// poll snapshot may predate: if this gather read plan progress just
		// before a task completed, a live frame already advanced Done, and a
		// wholesale replace with the older poll value would visibly revert it
		// until the next poll. Done is monotonic within a plan run, so take
		// the max per plan. Then clear the live-done dedup set — the poll is
		// the fresh baseline and already counts every persisted completion.
		for id, pollProg := range msg.snap.progress {
			if cur, ok := m.snap.progress[id]; ok && cur.Done > pollProg.Done {
				pollProg.Done = cur.Done
			}
			msg.snap.progress[id] = pollProg
		}
		m.snap.progress = msg.snap.progress
		m.liveDoneTasks = nil
	}
	if msg.snap.planEvent != nil {
		// MERGE the poll's events with the live-built tail rather than
		// replacing it. A wholesale replace would drop a live event whose DB
		// commit landed AFTER this poll's read began: that event was prepended
		// live but isn't in the poll snapshot, so an assign would permanently
		// lose it (it's a one-shot stream frame, never re-delivered). The
		// merge keeps both, deduped by id, so the poll reconciles WITHOUT
		// regressing the live tail.
		m.snap.planEvent = mergeEventTail(m.snap.planEvent, msg.snap.planEvent)
		m.snap.eventsHasMore = msg.snap.eventsHasMore
	}
	if msg.snap.tasks != nil {
		m.snap.tasks = msg.snap.tasks
		m.snap.tasksHasMore = msg.snap.tasksHasMore
	}
	// Descriptions are merged separately from tasks because they are fetched by
	// a separate query that can fail on its own: an older supervisor answers
	// CodeUnsupportedCommand, leaving this nil while tasks arrive fine. Keeping
	// the previous labels in that case beats blanking the column mid-session.
	if msg.snap.descriptions != nil {
		m.snap.descriptions = msg.snap.descriptions
	}
	if msg.snap.taskEvent != nil {
		m.snap.taskEvent = msg.snap.taskEvent
		m.snap.eventsHasMore = msg.snap.eventsHasMore
	}
	// Re-point the cursor at the SAME entity it was on before the refresh
	// (by ID), falling back to a clamp if that entity is gone. Without this,
	// a refresh that removes/reorders a row ahead of the cursor would leave
	// the (still in-bounds) index selecting a DIFFERENT entity than the
	// operator saw — so drilling in would open the wrong plan/task.
	m.reconcileCursor(selectedID)
	// Start the session-long live subscription now that the first gather has
	// landed (idempotent: only the first call starts it). This makes the
	// macro/meso event + state panes push-live, with this poll as the
	// reconcile net.
	if cmd := m.ensureAttach(); cmd != nil {
		return m, cmd
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
		// Reset the per-task log; the SESSION-long subscription (started at the
		// first fetch, see ensureAttach) keeps feeding — drilling in just changes
		// which pane it also fills. No per-drill start/stop.
		m.snap.live = nil
		m.viewport = viewportState{}
		return m, m.drillFetch()

	default:
		return m, nil
	}
}

// drillOut moves micro->meso->macro. The live Attach subscription is
// session-long (see ensureAttach), so drilling out does NOT stop it — the
// macro/meso views keep updating from the same stream; leaving micro just stops
// filling the per-task log pane.
func (m Model) drillOut() (tea.Model, tea.Cmd) {
	switch m.lvl {
	case levelMicro:
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

func decodeEvent(raw json.RawMessage) (ipc.AttachEvent, bool) {
	var event ipc.AttachEvent
	if err := json.Unmarshal(raw, &event); err != nil || event.Kind == "" {
		return ipc.AttachEvent{}, false
	}
	return event, true
}

// renderEvent formats one live event for the micro-view tail.
func renderEvent(ev ipc.AttachEvent) string {
	line := ev.Kind
	if ev.TaskID != "" {
		line += " task=" + ev.TaskID
	}
	if ev.Failure != nil {
		line += " failure=" + string(ev.Failure.Category)
	}
	return line
}

// taskDeltaStatus maps an event kind to the task status it implies, or "" if
// the kind doesn't directly change a task's status. It is the fast-path delta:
// the periodic poll re-reads the real status every tick and reconciles, so this
// only needs to cover the common lifecycle transitions to make the view feel
// live — an unmapped kind simply waits for the next poll.
func taskDeltaStatus(kind string) string {
	switch kind {
	case "task.claimed":
		return "running"
	case "task.done", "worker.completed", "worker.verified_done":
		return "done"
	case "task.failed", "task.failed_terminal", "worker.verification_failed":
		return "failed"
	case "task.released":
		return "ready"
	case "task.reclaimed":
		// The reaper requeued a task whose worker went away. Without this the
		// row keeps showing 'running' until the next poll — the one state that
		// is actively misleading, since it names a worker that is gone.
		return "pending"
	case "task.blocked", "task.context_requested":
		// The store emits these on the running→blocked transition (a worker
		// stalled or requested context). Reflect it immediately — a blocked
		// worker waiting on input is exactly the state an operator watching the
		// live view most needs to see promptly, not a poll interval later.
		return "blocked"
	default:
		return ""
	}
}

// macroEventCap bounds the macro-view live event tail.
const macroEventCap = 10

// prependEvent adds a live event to the newest-first macro event tail, deduped
// by id (a poll landing just after a live prepend re-includes the same row;
// dropping the duplicate keeps the pane from showing it twice) and capped at
// macroEventCap.
func prependEvent(tail []observe.Event, ev ipc.AttachEvent) []observe.Event {
	// Dedup by id — but ONLY for real (nonzero) ids. A store event always has a
	// nonzero autoincrement id; id==0 means a malformed/id-less frame, and every
	// such frame is distinct, so deduping them (they'd all collide on 0) would
	// wrongly drop all but the first. Keep them.
	if ev.ID != 0 {
		for i := range tail {
			if tail[i].ID == ev.ID {
				return tail // already present (from a poll or an earlier frame)
			}
		}
	}
	at := ev.OccurredAt
	if at.IsZero() {
		at = time.Now()
	}
	row := observe.Event{
		ID: ev.ID, PlanID: ev.PlanID, TaskID: ev.TaskID,
		Kind: ev.Kind, Stream: ev.Stream, OccurredAt: at, Failure: ev.Failure,
	}
	tail = append([]observe.Event{row}, tail...)
	if len(tail) > macroEventCap {
		tail = tail[:macroEventCap]
	}
	return tail
}

// mergeEventTail unions the live-built macro tail with a poll's fresh snapshot,
// deduped by id and kept newest-first (highest id first), capped at
// macroEventCap. Both inputs are already newest-first. It reconciles the pane
// toward the poll (the DB is truth) WITHOUT dropping a live event the poll's
// read predates — the union keeps that event; the cap trims the oldest. A live
// event still absent from the DB (impossible in practice — events are persisted
// before they stream) would simply age out as newer rows arrive.
func mergeEventTail(live, poll []observe.Event) []observe.Event {
	seen := make(map[int64]bool, len(live)+len(poll))
	// take reports whether to keep this row, recording real ids as seen. id==0
	// (a malformed/id-less frame) is never deduped — each is distinct, so
	// collapsing them on 0 would wrongly drop all but one.
	take := func(ev observe.Event) bool {
		if ev.ID == 0 {
			return true
		}
		if seen[ev.ID] {
			return false
		}
		seen[ev.ID] = true
		return true
	}
	merged := make([]observe.Event, 0, len(live)+len(poll))
	// Merge two newest-first lists by descending id, skipping duplicates.
	i, j := 0, 0
	for i < len(live) && j < len(poll) {
		var pick observe.Event
		if live[i].ID >= poll[j].ID {
			pick = live[i]
			i++
		} else {
			pick = poll[j]
			j++
		}
		if take(pick) {
			merged = append(merged, pick)
		}
	}
	for ; i < len(live); i++ {
		if take(live[i]) {
			merged = append(merged, live[i])
		}
	}
	for ; j < len(poll); j++ {
		if take(poll[j]) {
			merged = append(merged, poll[j])
		}
	}
	if len(merged) > macroEventCap {
		merged = merged[:macroEventCap]
	}
	return merged
}

// applyEvent applies a live event's TASK-STATUS delta to the snapshot so a
// lifecycle change is reflected immediately, ahead of the next poll. It is a
// pure function (returns the updated snapshot) to keep Update easy to test. A
// kind that maps to no task-status change, or a task_id not currently loaded, is
// a no-op; the poll reconciles everything regardless. The macro plan-PROGRESS
// delta is applied separately by Model.bumpLiveProgress (it needs the
// model-owned dedup set that spans drill levels).
func applyEvent(snap snapshot, ev ipc.AttachEvent) snapshot {
	status := taskDeltaStatus(ev.Kind)
	if status == "" || ev.TaskID == "" {
		return snap
	}
	for i := range snap.tasks {
		if snap.tasks[i].ID == ev.TaskID {
			snap.tasks[i].Status = status
			// A reclaim also releases the claim. Updating only Status leaves the
			// row rendering `w:<dead-worker>` beside `pending` until the next
			// poll -- naming a worker that is definitionally gone, which is
			// worse than the stale `running` this mapping was added to fix.
			if ev.Kind == "task.reclaimed" {
				snap.tasks[i].ClaimedByWorkerID = ""
			}
			break
		}
	}
	return snap
}

// bumpLiveProgress advances the plan's live Done counter for a fresh completion
// so the macro progress bar moves ahead of the next poll. It dedups by task-id
// via m.liveDoneTasks: a single completion emits TWO done-kind events
// (worker.completed then worker.verified_done), and at macro level snap.tasks is
// empty so a per-task status check can't tell them apart — the set counts each
// task at most once. Cleared on the next poll, which reconciles the exact count.
// A no-op for a non-done kind, an absent task/plan id, or an unknown plan.
func (m *Model) bumpLiveProgress(ev ipc.AttachEvent) {
	if taskDeltaStatus(ev.Kind) != "done" || ev.TaskID == "" || ev.PlanID == "" {
		return
	}
	if m.liveDoneTasks[ev.TaskID] {
		return // already counted this task's completion live
	}
	prog, ok := m.snap.progress[ev.PlanID]
	if !ok {
		return
	}
	if m.liveDoneTasks == nil {
		m.liveDoneTasks = map[string]bool{}
	}
	m.liveDoneTasks[ev.TaskID] = true
	if prog.Done < prog.Total {
		prog.Done++
		m.snap.progress[ev.PlanID] = prog
	}
}

// fetchDescriptions resolves the author-written labels for one plan's tasks in
// a single round trip.
//
// A failure here is deliberately non-fatal: the label is cosmetic, so a missing
// one degrades to showing the task id, while failing the whole view over it
// would turn a nicety into an outage. That also keeps an older supervisor
// (which answers CodeUnsupportedCommand) usable rather than blank.
func fetchDescriptions(
	ctx context.Context,
	source DataSource,
	projectID, planID string,
	taskIDs []string,
) map[string]string {
	if planID == "" || len(taskIDs) == 0 {
		return nil
	}
	got, err := source.TaskDescriptions(ctx, observe.TaskDescriptionsQuery{
		ProjectID: projectID,
		PlanID:    planID,
		TaskIDs:   taskIDs,
	})
	if err != nil {
		return nil
	}
	return got.ByTask
}

// taskIDsOf bounds a description fetch to exactly the tasks being rendered, so
// the label read stays as page-bounded as the snapshot that produced them.
func taskIDsOf(tasks []observe.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task.ID != "" {
			ids = append(ids, task.ID)
		}
	}
	return ids
}
