package tui

import (
	"context"
	"encoding/json"
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

// Init kicks off the first fetch and starts the refresh tick.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetchCmd(), tickCmd())
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
// produced while in levelMicro with a live subscription active.
type liveFrameMsg struct {
	raw json.RawMessage
}

// attachEndedMsg signals the Attach stream ended (cleanly or with error).
type attachEndedMsg struct {
	err error
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
		status, err := source.Status(ctx)
		if err != nil {
			return fetchedMsg{err: err}
		}

		snap := snapshot{status: status, progress: map[string]orch.Progress{}}
		for k, v := range prevProgress {
			snap.progress[k] = v
		}

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
func attachCmd(ctx context.Context, frames chan json.RawMessage, done chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case raw, ok := <-frames:
			if !ok {
				return attachEndedMsg{err: <-done}
			}
			return liveFrameMsg{raw: raw}
		case <-ctx.Done():
			return attachEndedMsg{err: ctx.Err()}
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
		return m, tea.Batch(m.fetchCmd(), tickCmd())

	case fetchedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
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
		return m, nil

	case liveFrameMsg:
		m.snap.live = append(m.snap.live, liveLogLine{at: time.Now(), text: renderFrame(msg.raw)})
		if len(m.snap.live) > 500 {
			m.snap.live = m.snap.live[len(m.snap.live)-500:]
		}
		return m, nil

	case attachEndedMsg:
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
		return m, m.fetchCmd()

	case levelMeso:
		if m.cursor >= len(m.snap.tasks) {
			return m, nil
		}
		m.selectedTask = m.snap.tasks[m.cursor]
		m.lvl = levelMicro
		m.snap.live = nil
		m.viewport = viewportState{}
		frames, done, cancel := startAttach(m.ctx, m.source)
		m.attachCancel = cancel
		return m, tea.Batch(m.fetchCmd(), attachCmd(m.ctx, frames, done))

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
		return m, m.fetchCmd()

	case levelMeso:
		m.lvl = levelMacro
		m.cursor = 0
		return m, m.fetchCmd()

	default:
		return m, nil
	}
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
