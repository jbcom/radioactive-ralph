//go:build gui

package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/lang"
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

	// painted is a test-only completion signal for a fully-rendered frame.
	painted chan<- struct{}

	// paintPump, startPaintPump, runWindow, quitApp, showWindow, and localize are
	// test-only seams for exercising the real-driver lifecycle without a native
	// window.
	paintPump      *paintPump
	startPaintPump func(func()) func()
	runWindow      func(fyne.Window)
	quitApp        func()
	showWindow     func()
	localize       func(string) string
	quitRequested  chan<- struct{}
	openRequested  chan<- struct{}
}

// lifecycleGroup owns every asynchronous operation launched by a ui. Close
// atomically stops new admission before cancellation and Wait, avoiding the
// WaitGroup Add/Wait race while guaranteeing no refresh/drive goroutine can
// escape Run.
type lifecycleGroup struct {
	mu      sync.Mutex
	closed  bool
	running sync.WaitGroup
}

func (g *lifecycleGroup) Go(fn func()) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.running.Add(1)
	go func() {
		defer g.running.Done()
		fn()
	}()
	return true
}

func (g *lifecycleGroup) Close() {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
}

func (g *lifecycleGroup) Wait() {
	g.running.Wait()
}

// paintRequest is handed off by a background gather and consumed exclusively
// by the live Fyne run loop through paintPump.Tick.
type paintRequest struct {
	paint func()
	done  chan bool
}

// paintPump is the production UI-thread boundary. Background goroutines enqueue
// completed snapshots and wait for their acknowledgement; they never call
// fyne.Do or fyne.DoAndWait. A forever Fyne animation invokes Tick from the
// driver's main loop. When a native/direct driver quit stops that loop, no
// callback can fall through to Fyne's post-drain "execute inline" behavior:
// ShowAndRun returns, uiCtx is cancelled, and blocked Dispatch calls exit.
type paintPump struct {
	queue   chan paintRequest
	queued  chan<- struct{} // test-only observation hook
	stopped atomic.Bool
}

func newPaintPump(queued chan<- struct{}) *paintPump {
	return &paintPump{queue: make(chan paintRequest, 64), queued: queued}
}

func (p *paintPump) Dispatch(ctx context.Context, paint func()) bool {
	if p.stopped.Load() {
		return false
	}
	req := paintRequest{paint: paint, done: make(chan bool, 1)}
	select {
	case p.queue <- req:
		if p.queued != nil {
			select {
			case p.queued <- struct{}{}:
			default:
			}
		}
	case <-ctx.Done():
		return false
	}

	select {
	case painted := <-req.done:
		return painted
	case <-ctx.Done():
		return false
	}
}

// Tick must only be called by Fyne's live run loop. It drains all snapshots
// currently ready so bursts of lifecycle events coalesce into one display
// frame instead of lagging behind the animation cadence.
func (p *paintPump) Tick() {
	for {
		select {
		case req := <-p.queue:
			painted := false
			if !p.stopped.Load() {
				req.paint()
				painted = true
			}
			req.done <- painted
		default:
			return
		}
	}
}

// Stop prevents all later ticks from executing callbacks and acknowledges work
// already queued. Dispatch callers racing Stop are also released by uiCtx
// cancellation in beginShutdown.
func (p *paintPump) Stop() {
	if p.stopped.Swap(true) {
		return
	}
	for {
		select {
		case req := <-p.queue:
			req.done <- false
		default:
			return
		}
	}
}

type uiRequest uint32

const uiRequestNone uiRequest = 0

const (
	uiRequestOpen uiRequest = 1 << iota
	uiRequestQuit
	uiRequestQuitStarted
	uiRequestReturned
)

const uiRequestTerminal = uiRequestQuitStarted | uiRequestReturned

// uiRequests is the lock-free boundary between callbacks that may execute
// after Fyne's native driver has drained and the still-live animation callback.
// Publishers only set bits. claim atomically chooses one request for the UI
// loop; quit wins whenever open and quit are in the same snapshot.
type uiRequests struct {
	state atomic.Uint32
}

func (r *uiRequests) publish(request uiRequest) bool {
	for {
		state := uiRequest(r.state.Load())
		if state&uiRequestTerminal != 0 {
			return false
		}
		if request == uiRequestOpen && state&uiRequestQuit != 0 {
			return false
		}
		if state&request != 0 {
			return false
		}
		if r.state.CompareAndSwap(uint32(state), uint32(state|request)) {
			return true
		}
	}
}

func (r *uiRequests) claim() uiRequest {
	for {
		state := uiRequest(r.state.Load())
		if state&uiRequestTerminal != 0 {
			return uiRequestNone
		}
		if state&uiRequestQuit != 0 {
			next := (state &^ (uiRequestOpen | uiRequestQuit)) | uiRequestQuitStarted
			if r.state.CompareAndSwap(uint32(state), uint32(next)) {
				return uiRequestQuit
			}
			continue
		}
		if state&uiRequestOpen != 0 {
			if r.state.CompareAndSwap(uint32(state), uint32(state&^uiRequestOpen)) {
				return uiRequestOpen
			}
			continue
		}
		return uiRequestNone
	}
}

// retire atomically discards every unconsumed request when ShowAndRun returns.
// Later systray callbacks are harmless no-ops even if Fyne invokes them inline
// from its already-drained callback queue.
func (r *uiRequests) retire() {
	for {
		state := uiRequest(r.state.Load())
		if state&uiRequestTerminal != 0 {
			return
		}
		if r.state.CompareAndSwap(uint32(state), uint32(uiRequestReturned)) {
			return
		}
	}
}

// Run builds and runs the Ralph desktop client: a system-tray entry plus a main
// window showing the macro→meso→micro drill of the supervisor's live state,
// with drive affordances (approve/pause/resume/abandon/kill/import). It blocks
// until the window closes (or, under the test driver, until the app stops).
func Run(ctx context.Context, o Opts) error {
	if o.Controller == nil {
		return fmt.Errorf("gui: Controller required")
	}

	parentCtx := ctx
	a := o.fyneApp
	if a == nil {
		a = app.NewWithID("com.jonbogaty.radioactive-ralph")
	}
	a.Settings().SetTheme(ralphTheme{})

	// Keep parent cancellation out of uiCtx until the live UI loop consumes the
	// request (or ShowAndRun returns naturally). This preserves the ordered
	// shutdown boundary: close admission, stop/drain the paint pump, then cancel
	// in-flight UI work. Values still flow through WithoutCancel.
	uiCtx, cancelUI := context.WithCancel(context.WithoutCancel(parentCtx))
	var background lifecycleGroup
	var activePaintPump *paintPump
	var shutdownOnce sync.Once
	beginShutdown := func() {
		shutdownOnce.Do(func() {
			background.Close()
			if activePaintPump != nil {
				activePaintPump.Stop()
			}
			cancelUI()
		})
	}
	defer func() {
		// A late headless-test paint can otherwise overlap the next Fyne app's
		// process-global text shaper. Stop new work, cancel in-flight I/O, and
		// join every UI goroutine before returning so neither test nor production
		// shutdown leaves a stale refresh touching a closed window.
		beginShutdown()
		background.Wait()
	}()

	w := a.NewWindow("radioactive-ralph")
	w.Resize(fyne.NewSize(920, 640))

	ui := newUI(uiCtx, o.Controller, o.ProjectID, w)
	ui.startAsync = background.Go
	ui.painted = o.painted
	w.SetContent(ui.root)

	// A supplied runWindow is the deterministic test seam for this real-driver
	// path. Ordinary headless tests retain the simpler show-and-wait path below.
	realDriverLifecycle := o.fyneApp == nil || o.runWindow != nil
	var pump *paintPump
	if realDriverLifecycle {
		pump = o.paintPump
		if pump == nil {
			pump = newPaintPump(nil)
		}
		activePaintPump = pump
		ui.paintDispatch = pump.Dispatch
	}

	quitApp := a.Quit
	if o.quitApp != nil {
		quitApp = o.quitApp
	}
	showWindow := w.Show
	if o.showWindow != nil {
		showWindow = o.showWindow
	}
	var requests uiRequests

	// publishQuit is shared by caller cancellation and the tray. Both sources
	// can run after Fyne's driver has drained, so this function only closes
	// Ralph-owned background admission and atomically publishes a request.
	publishQuit := func() {
		if !requests.publish(uiRequestQuit) {
			return
		}
		background.Close()
		if o.quitRequested != nil {
			select {
			case o.quitRequested <- struct{}{}:
			default:
			}
		}
	}

	// publishOpen is also safe after driver drain: it only sets a coalescing bit.
	// If quit is already pending or shutdown has started, the open is rejected.
	publishOpen := func() {
		if !requests.publish(uiRequestOpen) {
			return
		}
		if o.openRequested != nil {
			select {
			case o.openRequested <- struct{}{}:
			default:
			}
		}
	}

	// consumeRequest is the sole App.Quit and Window.Show call site. claim is
	// the linearization point: a quit present in the same atomic snapshot always
	// discards open; a later quit is consumed by the post-paint check in this
	// same frame.
	consumeRequest := func() bool {
		switch requests.claim() {
		case uiRequestQuit:
			beginShutdown()
			ui.quiescePaint()
			quitApp()
			return true
		case uiRequestOpen:
			showWindow()
		}
		return false
	}

	// The animation callback is the only request consumer. Check on both sides
	// of Tick so a request arriving during a paint is bounded to the same frame.
	runLoopTick := func() {
		if consumeRequest() {
			return
		}
		pump.Tick()
		consumeRequest()
	}

	if realDriverLifecycle {
		var stopPaintPump func()
		if o.startPaintPump != nil {
			stopPaintPump = o.startPaintPump(runLoopTick)
		} else {
			animation := fyne.NewAnimation(time.Second, func(float32) { runLoopTick() })
			animation.RepeatCount = fyne.AnimationRepeatForever
			animation.Start()
			stopPaintPump = animation.Stop
		}
		if stopPaintPump == nil {
			stopPaintPump = func() {}
		}
		defer stopPaintPump()
	}

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
		localize := func(label string) string { return lang.L(label) }
		if o.localize != nil {
			localize = o.localize
		}
		quitItem := fyne.NewMenuItem(localize("Quit"), publishQuit)
		// Fyne appends its own direct Driver.Quit item unless the supplied menu
		// explicitly identifies a quit item. Comparing an English label is not
		// sufficient under non-English locales.
		quitItem.IsQuit = true
		menu := fyne.NewMenu("radioactive-ralph",
			fyne.NewMenuItem("Open Ralph", publishOpen),
			// Once the window is hidden to the tray, the menu is the only GUI
			// affordance left — it MUST offer a way to quit, or the app can only
			// be killed from a terminal.
			quitItem,
		)
		desk.SetSystemTrayMenu(menu)
		// Closing the window hides to tray rather than quitting, so the ambient
		// affordance stays alive.
		w.SetCloseIntercept(func() { w.Hide() })
	}

	// Drive the periodic refresh and the live event subscription on their own
	// goroutines; both end when ctx is cancelled (window close / app stop).
	background.Go(func() { ui.runRefresh(uiCtx) })
	background.Go(func() { ui.runAttach(uiCtx) })

	var showExited chan struct{}
	if realDriverLifecycle {
		showExited = make(chan struct{})
		// App.Quit closes windows before destroying the driver. SetOnClosed is
		// therefore the earliest common hook for tray Quit and app shutdown.
		// Direct Driver.Quit paths (including Fyne's own signal catcher) do not
		// close windows; paintPump makes those safe until ShowAndRun returns.
		w.SetOnClosed(beginShutdown)
		// Register the semantic pre-stop hook as well. Pinned Fyne v2.8 may drain
		// this callback instead of executing it, so correctness does not depend
		// on it; once Fyne runs OnStopped synchronously before driver drain, this
		// stops the pump at the earliest lifecycle boundary.
		a.Lifecycle().SetOnStopped(beginShutdown)

		background.Go(func() {
			waitForShowExitOrCancellation(parentCtx, showExited, func() {
				publishQuit()
			})
		})
	}

	// First paint runs on its own goroutine, NOT inline: refreshNow does a
	// blocking Status IPC, so calling it synchronously here would keep the
	// window from ever appearing if the supervisor accepts the connection but is
	// slow or never answers. The window shows immediately (empty), then the
	// snapshot fills it in — the same async path the ticker uses. In sync mode
	// (tests) it stays inline so the first render is deterministic.
	if !realDriverLifecycle && ui.syncRender {
		ui.refreshNow()
	} else {
		background.Go(ui.refreshNow)
	}

	if realDriverLifecycle {
		if o.runWindow != nil {
			o.runWindow(w)
		} else {
			w.ShowAndRun()
		}
		requests.retire()
		// Close admission synchronously at the lifecycle boundary; do not leave
		// that transition to a deferred cleanup or watcher scheduling.
		beginShutdown()
		close(showExited)
		return nil
	}
	// Test driver: show and block until the caller cancels ctx, so the refresh
	// and attach goroutines are joined (they exit on ctx.Done) and Run's
	// lifecycle matches the real ShowAndRun.
	w.Show()
	<-parentCtx.Done()
	return nil
}

// waitForShowExitOrCancellation owns the real driver's shutdown race. Natural
// exit always wins if showExited is already ready, including when cancellation
// is also ready; the second check closes the race between the precheck and the
// blocking select.
func waitForShowExitOrCancellation(ctx context.Context, showExited <-chan struct{}, quit func()) {
	select {
	case <-showExited:
		return
	default:
	}

	select {
	case <-showExited:
	case <-ctx.Done():
		select {
		case <-showExited:
			return
		default:
			quit()
		}
	}
}

// ui holds the window, the controller, and the mutable view state. All widget
// mutation happens on Fyne's main thread via paintDispatch (see refreshNow).
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
	// drill in/out. Main-thread-only (render is always called by paintDispatch),
	// so no lock is needed. Empty until the first render.
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

	// paintMu serializes widget layout even when the injected headless driver
	// executes paint callbacks inline on multiple caller goroutines. The real
	// driver already serializes its UI loop, but the test driver does not, and
	// Fyne's package-global HarfBuzz shaper reuses a mutable buffer. Gather
	// remains concurrent; refreshSeq still drops stale frames after they enter.
	paintMu sync.Mutex

	// paintDispatch is completion-aware. Production points it at paintPump, whose
	// Tick runs only inside the live Fyne event loop. Headless ui unit tests use
	// dispatchThroughFyne; their test driver executes callbacks inline and paintMu
	// serializes the package-global HarfBuzz shaper.
	paintDispatch func(context.Context, func()) bool

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

	// startAsync admits every refresh/drive goroutine into Run's lifecycle
	// group. Once shutdown closes admission, taps and late callbacks cannot
	// launch work that would outlive the Fyne app.
	startAsync func(func()) bool

	// painted is a test-only signal sent after a frame has fully rendered.
	painted chan<- struct{}

	// paintHook is a test-only observer called on entry/exit of the serialized
	// paint section. Production leaves it nil.
	paintHook func(enter bool)

	// paintAttemptHook is a test-only pre-lock barrier used to prove concurrent
	// refreshes reached the paint section before paintMu serialized them.
	paintAttemptHook func()
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
		paintDispatch:    dispatchThroughFyne,
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
// attach goroutine), then hands it to a completion-aware Fyne dispatcher to
// render. Keeping every blocking read off the UI thread means a slow or
// unavailable socket can never freeze the window — the worst case is a stale
// view, not a hung one.
func (u *ui) refreshNow() {
	if u.ctx.Err() != nil {
		return
	}

	// Snapshot the drill selection AND claim an ordering seq under the lock (the
	// selection is written by tap handlers on the main thread; this is the one
	// cross-thread read).
	u.mu.Lock()
	plan, task := u.selectedPlan, u.selectedTask
	u.refreshSeq++
	seq := u.refreshSeq
	u.mu.Unlock()

	snap := u.gather(plan, task)
	if u.ctx.Err() != nil {
		return
	}

	paint := func() {
		if u.paintAttemptHook != nil {
			u.paintAttemptHook()
		}
		u.paintMu.Lock()
		defer u.paintMu.Unlock()
		if u.paintHook != nil {
			u.paintHook(true)
			defer u.paintHook(false)
		}
		if u.ctx.Err() != nil {
			return
		}

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
		if u.painted != nil {
			select {
			case u.painted <- struct{}{}:
			default:
			}
		}
	}
	if u.syncRender {
		paint() // tests: render inline so assertions see it immediately
		return
	}
	u.paintDispatch(u.ctx, paint)
}

// dispatchThroughFyne exists only for injected headless-app tests. Production
// installs paintPump.Dispatch before launching background work, so a direct
// driver stop can never execute Ralph widget mutation inline after drain.
func dispatchThroughFyne(ctx context.Context, paint func()) bool {
	acknowledged := make(chan struct{})
	fyne.DoAndWait(func() {
		defer close(acknowledged)
		paint()
	})
	select {
	case <-acknowledged:
		return true
	case <-ctx.Done():
		return false
	default:
		return false
	}
}

// quiescePaint waits for a paint that passed its cancellation check to finish.
// Once the lifecycle group is closed and u.ctx is cancelled, later callbacks
// may still be acknowledged by Fyne but will return before touching widgets.
func (u *ui) quiescePaint() {
	u.paintMu.Lock()
	u.paintMu.Unlock()
}

func (u *ui) goAsync(fn func()) {
	if u.startAsync != nil {
		u.startAsync(fn)
		return
	}
	go fn()
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
