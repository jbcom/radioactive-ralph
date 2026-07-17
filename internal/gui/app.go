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

	// System tray (where the desktop driver supports it): a compact way to
	// raise the window and quit. Degrades to just the window otherwise.
	if desk, ok := a.(desktop.App); ok {
		menu := fyne.NewMenu("radioactive-ralph",
			fyne.NewMenuItem("Open Ralph", func() { w.Show() }),
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

	ui.refreshNow() // first paint before the ticker's first tick

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
	body      *fyne.Container // swapped per drill level
	errBanner *widget.Label

	// mu guards the drill selection, which is written by tap handlers on the
	// main thread and read by gather() on the refresh/attach goroutine.
	mu           sync.Mutex
	selectedPlan string
	selectedTask string

	// syncRender, when set (tests only), makes refreshNow/drive/drillTo run inline
	// and synchronously (no goroutine, no fyne.Do queueing) so a test can tap a
	// button and immediately assert the result. Production is always async.
	syncRender bool
}

func newUI(ctx context.Context, c Controller, project string, w fyne.Window) *ui {
	u := &ui{
		ctx:       ctx,
		ctrl:      c,
		project:   project,
		win:       w,
		header:    widget.NewLabel(""),
		body:      container.NewVBox(),
		errBanner: widget.NewLabel(""),
	}
	u.errBanner.Hide()
	u.root = container.NewBorder(
		container.NewVBox(u.header, u.errBanner), // top
		nil, nil, nil,
		container.NewVScroll(u.body), // center
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

// runAttach subscribes to the live event stream; each event triggers an
// immediate refresh so the view feels live between ticks. Ends on ctx cancel.
func (u *ui) runAttach(ctx context.Context) {
	_ = u.ctrl.Attach(ctx, func(json.RawMessage) error {
		u.refreshNow()
		return nil
	})
}

// refreshNow gathers a complete data snapshot for the current drill level OFF
// the Fyne main thread (all the IPC/store reads happen here, on the refresh or
// attach goroutine), then hands it to fyne.Do to render. Keeping every blocking
// read off the UI thread means a slow or unavailable socket can never freeze the
// window — the worst case is a stale view, not a hung one.
func (u *ui) refreshNow() {
	// Snapshot the drill selection under the lock (it is written by tap handlers
	// on the main thread; this is the one cross-thread read).
	u.mu.Lock()
	plan, task := u.selectedPlan, u.selectedTask
	u.mu.Unlock()

	snap := u.gather(plan, task)

	paint := func() {
		u.setError(snap.err)
		u.header.SetText(headerText(snap.status))
		u.render(snap)
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

	plans    []store.Plan
	progress map[string]orch.Progress // planID -> progress (macro)
	tasks    []store.Task             // meso
	events   []store.Event            // micro
	killID   string                   // micro: worker id running the selected task ("" = none)
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
		for _, w := range st.Workers {
			if w.PlanID == plan && w.TaskID == task {
				s.killID = w.WorkerID // store worker-row id — the kill key
				break
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
	}
	return s
}

func (u *ui) setError(err error) {
	if err == nil {
		u.errBanner.SetText("")
		u.errBanner.Hide()
		return
	}
	u.errBanner.SetText("error: " + err.Error())
	u.errBanner.Show()
}
