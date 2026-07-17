//go:build gui

package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// refreshInterval is how often the GUI re-fetches Status + the current view's
// data. Matches the TUI's cadence: live enough, not a socket hammer.
const refreshInterval = 1 * time.Second

// Opts configures Run.
type Opts struct {
	Controller Controller // required: the read+drive seam
	ProjectID  string     // scopes plan/event reads (empty = all projects)

	// fyneApp overrides app.New() — the headless test driver passes test.NewApp()
	// here so view/launch tests run with no display. Nil = real desktop app.
	fyneApp fyne.App
}

// Run builds and runs the Ralph desktop client: a system-tray entry plus a main
// window showing the macro→meso→micro drill of the supervisor's live state,
// with drive affordances (approve/pause/resume/abandon/kill/import). It blocks
// until the window closes (or, under the test driver, until the app stops).
func Run(ctx context.Context, o Opts) error {
	if o.Controller == nil {
		return fmt.Errorf("gui: Controller required")
	}

	a := o.fyneApp
	if a == nil {
		a = app.NewWithID("com.jonbogaty.radioactive-ralph")
	}
	a.Settings().SetTheme(ralphTheme{})

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	w := a.NewWindow("radioactive-ralph")
	w.Resize(fyne.NewSize(920, 640))

	ui := newUI(ctx, o.Controller, o.ProjectID, w)
	w.SetContent(ui.root)

	// Keyboard navigation (a11y + parity with the TUI, which is fully keyboard-
	// driven): Escape drills back one level (micro→meso→macro), the mouse-free
	// equivalent of the on-screen back buttons. Fyne routes TypedKey to the
	// FOCUSED widget first, so SetOnTypedKey alone misses Escape when a plan/task
	// button has focus. The desktop canvas's SetOnKeyDown fires for every key
	// regardless of focus, so prefer it and fall back to SetOnTypedKey where the
	// desktop canvas isn't available (e.g. the headless test driver).
	if dc, ok := w.Canvas().(desktop.Canvas); ok {
		dc.SetOnKeyDown(func(ev *fyne.KeyEvent) {
			if ev.Name == fyne.KeyEscape {
				ui.drillBack()
			}
		})
	} else {
		w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
			if ev.Name == fyne.KeyEscape {
				ui.drillBack()
			}
		})
	}

	// System tray (where the desktop driver supports it): a compact way to
	// raise the window and quit. Degrades to just the window otherwise.
	if desk, ok := a.(desktop.App); ok {
		menu := fyne.NewMenu("radioactive-ralph",
			fyne.NewMenuItem("Open Ralph", func() { w.Show() }),
			// Once the window is hidden to the tray, the menu is the only GUI
			// affordance left — it MUST offer a way to quit, or the app can only
			// be killed from a terminal.
			fyne.NewMenuItem("Quit", func() { a.Quit() }),
		)
		desk.SetSystemTrayMenu(menu)
		// Closing the window hides to tray rather than quitting, so the ambient
		// affordance stays alive.
		w.SetCloseIntercept(func() { w.Hide() })
	}

	// Drive the periodic refresh and the live event subscription on their own
	// goroutines; both end when ctx is cancelled (window close / app stop).
	go ui.runRefresh(ctx)
	go ui.runAttach(ctx)

	// If the caller cancels ctx (signal, supervisor shutdown), tear the window
	// down too — otherwise ShowAndRun would keep a stale, non-functional window
	// on screen after the background goroutines have already exited.
	if o.fyneApp == nil {
		go func() {
			<-ctx.Done()
			fyne.Do(func() { a.Quit() })
		}()
	}

	// First paint runs on its own goroutine, NOT inline: refreshNow does a
	// blocking Status IPC, so calling it synchronously here would keep the
	// window from ever appearing if the supervisor accepts the connection but is
	// slow or never answers. The window shows immediately (empty), then the
	// snapshot fills it in — the same async path the ticker uses. In sync mode
	// (tests) it stays inline so the first render is deterministic.
	if o.fyneApp != nil && ui.syncRender {
		ui.refreshNow()
	} else {
		go ui.refreshNow()
	}

	if o.fyneApp == nil {
		w.ShowAndRun() // real app: blocks until quit; window close cancels ctx via defer
		return nil
	}
	// Test driver: show and block until the caller cancels ctx, so the refresh
	// and attach goroutines are joined (they exit on ctx.Done) and Run's
	// lifecycle matches the real ShowAndRun.
	w.Show()
	<-ctx.Done()
	return nil
}

// ui holds the window, the controller, and the mutable view state. All widget
// mutation happens on Fyne's main thread via fyne.Do (see refreshNow).
type ui struct {
	ctx     context.Context
	ctrl    Controller
	project string
	win     fyne.Window

	root      *fyne.Container
	header    *widget.Label
	body      *fyne.Container   // swapped per drill level
	scroll    *container.Scroll // wraps body; scrolled to top on each drill
	errBanner *widget.Label

	// firstFocusable is the first keyboard-focusable widget of the view built
	// during the current render — the back button at meso/micro, else the first
	// plan/task button. render() focuses it ONLY when the drill view just changed
	// (see focusedView) so a keyboard-only operator lands on an actionable control
	// on arrival without blind-Tabbing. Reset to nil at the top of each render; a
	// view with no buttons leaves it nil and render() focuses nothing.
	firstFocusable fyne.Focusable

	// focusedView identifies the drill view (level+selection) whose initial focus
	// has already been set. render() runs on every 1s tick and live event, not
	// just on navigation, so focusing unconditionally would yank focus back to the
	// first control every refresh — stealing it from a keyboard operator mid-Tab.
	// We only (re)initialize focus when this identity changes, i.e. on an actual
	// drill in/out. Main-thread-only (render is always called under fyne.Do), so
	// no lock is needed. Empty until the first render.
	focusedView string

	// mu guards the drill selection, which is written by tap handlers on the
	// main thread and read by gather() on the refresh/attach goroutine. It also
	// guards refreshSeq and actionErr (below).
	mu           sync.Mutex
	selectedPlan string
	selectedTask string

	// actionErr holds the last failed drive action's message ("" = none). Drive
	// errors need their own slot because they don't come from the Status snapshot:
	// a bare fyne.Do(showErr) would be silently erased by the next tick's
	// setError(snap.err=nil), so a transient "kill failed" could flash and vanish
	// or, conversely, never clear. paint() renders actionErr when set (it takes
	// precedence over a Status error, since it's the thing the operator just did),
	// and any subsequent successful drive or drill clears it. Guarded by mu.
	actionErr string

	// viewToken increments on every drill (drillTo/drillBack). A drive() captures
	// it when the action starts and records its outcome only if the token is still
	// current when the (off-thread) RPC returns — so an in-flight action that
	// completes AFTER the operator has navigated away neither resurrects a banner
	// on, nor clobbers the state of, the view they moved to. Guarded by mu.
	viewToken uint64

	// importing is set while the transient Import-plan form is on screen. That
	// form is built imperatively (not from a snapshot), so a periodic paint's
	// u.render(snap) would wipe it — and any pasted text — mid-edit. paint() skips
	// the render step while importing is set; drills clear it. Guarded by mu.
	importing bool

	// refreshSeq orders concurrent refreshes. refreshNow is fired from four
	// sources (1s ticker, each live event, each drive, each drill); their
	// off-thread gather()s can finish out of order, so a slow older gather could
	// repaint stale data (even a drill level the user already left) after a newer
	// one. Each refreshNow claims an incrementing seq; paint() no-ops if a newer
	// seq has already painted. Guarded by mu.
	refreshSeq     uint64
	lastPaintedSeq uint64

	// syncRender, when set (tests only), makes refreshNow/drive/drillTo run inline
	// and synchronously (no goroutine, no fyne.Do queueing) so a test can tap a
	// button and immediately assert the result. Production is always async.
	syncRender bool

	// attachRetryDelay is how long runAttach waits before re-dialing the live
	// event stream after it ends (see runAttach). A per-ui field (not a package
	// var) so a test can shrink it on its OWN ui without racing another test's
	// still-running runAttach goroutine reading a shared global. Defaults to
	// defaultAttachRetryDelay in newUI.
	attachRetryDelay time.Duration
}

func newUI(ctx context.Context, c Controller, project string, w fyne.Window) *ui {
	u := &ui{
		ctx:              ctx,
		ctrl:             c,
		project:          project,
		win:              w,
		header:           widget.NewLabel(""),
		body:             container.NewVBox(),
		errBanner:        widget.NewLabel(""),
		attachRetryDelay: defaultAttachRetryDelay,
	}
	u.errBanner.Hide()
	u.scroll = container.NewVScroll(u.body)
	u.root = container.NewBorder(
		container.NewVBox(u.header, u.errBanner), // top
		nil, nil, nil,
		u.scroll, // center
	)
	return u
}

// runRefresh ticks refreshNow every refreshInterval until ctx is done.
func (u *ui) runRefresh(ctx context.Context) {
	t := time.NewTicker(refreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.refreshNow()
		}
	}
}

// defaultAttachRetryDelay is the production value for ui.attachRetryDelay: how
// long runAttach waits before re-dialing the live event stream after it ends
// (supervisor not up yet, restarted, or dropped the socket). Short enough that
// the stream feels continuous across a supervisor blip, long enough not to
// hammer the socket while the supervisor is down; the 1s ticker keeps the view
// fresh in the meantime regardless.
const defaultAttachRetryDelay = 1 * time.Second

// runAttach subscribes to the live event stream; each event triggers an
// immediate refresh so the view feels live between ticks. Attach returns on ANY
// stream end — a failed dial (supervisor not up yet), an EOF (supervisor
// restarts or drops the socket), or a decode error — so this RE-DIALS in a loop
// until ctx is cancelled. Without the loop the stream was single-shot: the first
// pre-supervisor launch or supervisor restart killed it permanently for the rest
// of the session, silently degrading the GUI to 1s polling with no recovery.
func (u *ui) runAttach(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		_ = u.ctrl.Attach(ctx, func(raw json.RawMessage) error {
			// Only refresh for events that change visible aggregate state
			// (task/plan/worker lifecycle). Skipping pure log/heartbeat kinds
			// (tick, task.progress) avoids a full-snapshot re-read storm on a
			// busy stream — the GUI re-reads everything from the store, so a
			// per-frame refresh for a heartbeat would be pure waste.
			if eventTriggersRefresh(raw) {
				u.refreshNow()
			}
			return nil
		})
		// Attach returned: the stream ended. Back off briefly, then reconnect —
		// unless the app is shutting down.
		select {
		case <-ctx.Done():
			return
		case <-time.After(u.attachRetryDelay):
		}
	}
}

// refreshNoiseKinds are event kinds that do NOT change any aggregate the GUI
// renders (plan/task/worker/status counts), so they must not trigger a full
// refresh. Everything else does — a live view should reflect any lifecycle
// change immediately, and the periodic poll reconciles anything a skipped kind
// might have implied.
var refreshNoiseKinds = map[string]bool{
	"tick":          true, // supervisor heartbeat
	"task.progress": true, // mid-turn progress, not a state change
}

// eventTriggersRefresh reports whether a live Attach frame should trigger a GUI
// refresh. An undecodable frame defaults to true: if we can't tell what it is,
// refreshing is the safe (merely-wasteful) choice over silently going stale.
func eventTriggersRefresh(raw json.RawMessage) bool {
	var ev ipc.AttachEvent
	if err := json.Unmarshal(raw, &ev); err != nil || ev.Kind == "" {
		return true
	}
	return !refreshNoiseKinds[ev.Kind]
}

// refreshNow gathers a complete data snapshot for the current drill level OFF
// the Fyne main thread (all the IPC/store reads happen here, on the refresh or
// attach goroutine), then hands it to fyne.Do to render. Keeping every blocking
// read off the UI thread means a slow or unavailable socket can never freeze the
// window — the worst case is a stale view, not a hung one.
func (u *ui) refreshNow() {
	// Snapshot the drill selection AND claim an ordering seq under the lock (the
	// selection is written by tap handlers on the main thread; this is the one
	// cross-thread read).
	u.mu.Lock()
	plan, task := u.selectedPlan, u.selectedTask
	u.refreshSeq++
	seq := u.refreshSeq
	u.mu.Unlock()

	snap := u.gather(plan, task)

	paint := func() {
		// Drop a stale paint: if a newer refresh already painted, this gather's
		// data is out of date (possibly a drill level the user already left).
		u.mu.Lock()
		if seq < u.lastPaintedSeq {
			u.mu.Unlock()
			return
		}
		u.lastPaintedSeq = seq
		// A failed drive action's message takes precedence over a Status error —
		// it's the thing the operator just did — and persists across data refreshes
		// until a successful drive/drill clears it.
		actionErr := u.actionErr
		importing := u.importing
		u.mu.Unlock()

		switch {
		case actionErr != "":
			u.setBanner(actionErr)
		case snap.err != nil:
			u.setBanner("error: " + snap.err.Error())
		default:
			u.setBanner("")
		}
		u.header.SetText(headerText(snap.status, snap.err))
		// While the transient import form is up, refresh the header/banner (so
		// liveness and errors still update) but do NOT rebuild the body — that
		// form is built imperatively, not from a snapshot, so re-rendering would
		// wipe it and any half-typed plan text. A drill or a completed import
		// clears importing and normal rendering resumes.
		if !importing {
			u.render(snap)
		}
	}
	if u.syncRender {
		paint() // tests: render inline so assertions see it immediately
		return
	}
	fyne.Do(paint)
}

// snapshot is one fully-gathered view state: the status plus exactly the data
// the current drill level renders. All fields are filled off the main thread by
// gather; render() only reads them.
type snapshot struct {
	level        drillLevel
	selectedPlan string
	selectedTask string
	status       ipc.StatusReply
	err          error

	plans      []store.Plan
	progress   map[string]orch.Progress // planID -> progress (macro)
	projEvents []store.Event            // macro: recent project-wide events
	tasks      []store.Task             // meso
	events     []store.Event            // micro
	killID     string                   // micro: worker id running the selected task ("" = none)
}

type drillLevel int

const (
	levelMacro drillLevel = iota
	levelMeso
	levelMicro
)

// gather performs all reads for the drill level implied by (plan, task) off the
// main thread and returns a render-ready snapshot. The first error encountered
// is recorded in snapshot.err (surfaced as a banner) but never aborts the whole
// gather — a partial view beats a blank one.
func (u *ui) gather(plan, task string) snapshot {
	s := snapshot{selectedPlan: plan, selectedTask: task}
	st, err := u.ctrl.Status(u.ctx)
	s.status = st
	s.err = err

	switch {
	case plan != "" && task != "":
		s.level = levelMicro
		s.events, _ = u.ctrl.ListTaskEvents(u.ctx, plan, task, 50)
		// The kill key is the worker that CLAIMED this task. Read it from the
		// task's own claimed_by_worker_id, which is authoritative even for a
		// native-fanout group where one worker claims several tasks but the
		// worker row's current_task_id records only the first — so the kill
		// affordance appears on every task the worker holds, not just the first.
		// Fall back to the status Workers scan if the task row is unavailable.
		if tasks, terr := u.ctrl.ListTasks(u.ctx, plan); terr == nil {
			for _, t := range tasks {
				if t.ID == task && t.Status == store.TaskStatusRunning {
					s.killID = t.ClaimedByWorkerID
					break
				}
			}
		}
		if s.killID == "" {
			for _, w := range st.Workers {
				if w.PlanID == plan && w.TaskID == task {
					s.killID = w.WorkerID // store worker-row id — the kill key
					break
				}
			}
		}
	case plan != "":
		s.level = levelMeso
		s.tasks, _ = u.ctrl.ListTasks(u.ctx, plan)
	default:
		s.level = levelMacro
		s.plans, _ = u.ctrl.ListPlans(u.ctx, u.project)
		s.progress = make(map[string]orch.Progress, len(s.plans))
		for _, p := range s.plans {
			pr, _ := u.ctrl.PlanProgress(u.ctx, p.ID)
			s.progress[p.ID] = pr
		}
		// The ambient project-activity feed the TUI's macro view also shows.
		s.projEvents, _ = u.ctrl.ListProjectEvents(u.ctx, u.project, 20)
	}
	return s
}

// setBanner shows msg in the error banner, or hides it when msg is empty. The
// single entry point for both Status-connection errors and drive-action errors
// so exactly one of them is visible at a time (see paint's precedence). Main
// thread only.
func (u *ui) setBanner(msg string) {
	if msg == "" {
		u.errBanner.SetText("")
		u.errBanner.Hide()
		return
	}
	u.errBanner.SetText(msg)
	u.errBanner.Show()
}
