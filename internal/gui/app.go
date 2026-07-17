//go:build gui

package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
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

	// drill state: which plan/task is selected (empty = macro level).
	selectedPlan string
	selectedTask string
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

// refreshNow re-fetches Status plus the current drill level's data and rebuilds
// the view on Fyne's main thread.
func (u *ui) refreshNow() {
	st, err := u.ctrl.Status(u.ctx)
	fyne.Do(func() {
		u.setError(err)
		u.header.SetText(headerText(st))
		u.rebuildBody()
	})
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
