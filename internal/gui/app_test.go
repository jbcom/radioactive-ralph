//go:build gui

package gui

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// guiWaitBudget is the deadline for every POSITIVE wait in this file -- "did the
// window open", "did Run return", "did the first frame paint".
//
// It is deliberately far larger than the work these waits observe, because the
// assertion is "does this EVER happen", not "does it happen within N". No
// product requirement says the first frame lands in 3s, and a headless CI
// runner is slower and noisier than any dev machine.
//
// The former 3s value failed TestRun_StartsAndStopsCleanly on ubuntu-latest
// while passing 3/3 locally -- the tell that a threshold is measuring runner
// speed rather than correctness. A hang still fails here, just later, and a
// test that needs 30s to report a real hang beats one that reports a phantom
// hang on a busy runner.
//
// NOT used for negative assertions ("this must NOT complete yet"), where a
// short timeout IS the assertion; the one such wait in this file keeps its own
// deliberately-brief budget.
const guiWaitBudget = 30 * time.Second

type blockingStatusController struct {
	*fakeController
	statusStarted chan struct{}
	statusRelease chan struct{}
}

func (b *blockingStatusController) Snapshot(
	ctx context.Context,
	query observe.SnapshotQuery,
) (*observe.Snapshot, error) {
	close(b.statusStarted)
	<-b.statusRelease
	return b.fakeController.Snapshot(ctx, query)
}

type contextCapturingController struct {
	*fakeController
	statusCtx chan context.Context
	once      sync.Once
}

func (c *contextCapturingController) Snapshot(
	ctx context.Context,
	query observe.SnapshotQuery,
) (*observe.Snapshot, error) {
	c.once.Do(func() { c.statusCtx <- ctx })
	return c.fakeController.Snapshot(ctx, query)
}

type lifecycleRefreshController struct {
	*contextCapturingController
	refreshEvent chan struct{}
}

func (c *lifecycleRefreshController) Attach(
	ctx context.Context,
	onEvent func(ipc.AttachEvent) error,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.refreshEvent:
		if err := onEvent(ipc.AttachEvent{Kind: "task.done"}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}
}

type directDrainController struct {
	*lifecycleRefreshController
	statusCalls        atomic.Int32
	lateGatherStarted  chan struct{}
	lateGatherReleased chan struct{}
}

func (c *directDrainController) Snapshot(
	ctx context.Context,
	query observe.SnapshotQuery,
) (*observe.Snapshot, error) {
	if c.statusCalls.Add(1) == 2 {
		close(c.lateGatherStarted)
		<-c.lateGatherReleased
	}
	return c.lifecycleRefreshController.Snapshot(ctx, query)
}

type capturingDesktopApp struct {
	fyne.App
	menuSet chan *fyne.Menu
}

func (a *capturingDesktopApp) SetSystemTrayMenu(menu *fyne.Menu) {
	a.menuSet <- menu
}

func (*capturingDesktopApp) SetSystemTrayIcon(fyne.Resource) {}

func (*capturingDesktopApp) SetSystemTrayWindow(fyne.Window) {}

type trayLifecycleHarness struct {
	cancelParent context.CancelFunc
	menu         *fyne.Menu
	tick         func()

	beginDriverDrain chan struct{}
	driverDrained    chan struct{}
	showCanReturn    chan struct{}
	beginDrainOnce   sync.Once
	returnOnce       sync.Once

	runDone chan struct{}
	runErr  error

	quitCount atomic.Int32
	showCount atomic.Int32
}

func newTrayLifecycleHarness(t *testing.T) *trayLifecycleHarness {
	t.Helper()

	parentCtx, cancelParent := context.WithCancel(context.Background())
	baseApp := test.NewApp()
	t.Cleanup(baseApp.Quit)
	app := &capturingDesktopApp{App: baseApp, menuSet: make(chan *fyne.Menu, 1)}
	h := &trayLifecycleHarness{
		cancelParent:     cancelParent,
		beginDriverDrain: make(chan struct{}),
		driverDrained:    make(chan struct{}),
		showCanReturn:    make(chan struct{}),
		runDone:          make(chan struct{}),
	}
	windowRunning := make(chan struct{})

	go func() {
		h.runErr = Run(parentCtx, Opts{
			Controller: newFakeController(),
			fyneApp:    app,
			paintPump:  newPaintPump(nil),
			startPaintPump: func(runLoopTick func()) func() {
				h.tick = runLoopTick
				return func() {}
			},
			runWindow: func(fyne.Window) {
				close(windowRunning)
				<-h.beginDriverDrain
				close(h.driverDrained)
				<-h.showCanReturn
			},
			quitApp: func() {
				h.quitCount.Add(1)
				h.beginDrainOnce.Do(func() { close(h.beginDriverDrain) })
				h.returnOnce.Do(func() { close(h.showCanReturn) })
			},
			showWindow: func() {
				h.showCount.Add(1)
			},
		})
		close(h.runDone)
	}()

	select {
	case h.menu = <-app.menuSet:
	case <-time.After(guiWaitBudget):
		h.release()
		t.Fatal("desktop app did not receive a system tray menu")
	}
	select {
	case <-windowRunning:
	case <-time.After(guiWaitBudget):
		h.release()
		t.Fatal("forced real-driver window path did not start")
	}

	t.Cleanup(func() {
		cancelParent()
		h.release()
		select {
		case <-h.runDone:
		case <-time.After(guiWaitBudget):
			t.Error("tray lifecycle harness did not stop")
		}
	})
	return h
}

func (h *trayLifecycleHarness) drain(t *testing.T) {
	t.Helper()
	h.beginDrainOnce.Do(func() { close(h.beginDriverDrain) })
	select {
	case <-h.driverDrained:
	case <-time.After(guiWaitBudget):
		t.Fatal("forced driver did not reach its internally-drained state")
	}
}

func (h *trayLifecycleHarness) release() {
	h.beginDrainOnce.Do(func() { close(h.beginDriverDrain) })
	h.returnOnce.Do(func() { close(h.showCanReturn) })
}

func (h *trayLifecycleHarness) wait(t *testing.T) {
	t.Helper()
	select {
	case <-h.runDone:
		if h.runErr != nil {
			t.Fatalf("Run returned error: %v", h.runErr)
		}
	case <-time.After(guiWaitBudget):
		t.Fatal("Run did not return")
	}
}

func newLifecycleRefreshController() *lifecycleRefreshController {
	return &lifecycleRefreshController{
		contextCapturingController: &contextCapturingController{
			fakeController: newFakeController(),
			statusCtx:      make(chan context.Context, 1),
		},
		refreshEvent: make(chan struct{}, 1),
	}
}

// TestRun_StartsAndStopsCleanly launches the GUI under the headless test driver
// with a fake controller, confirms it painted the initial macro view, and shuts
// down cleanly when the context is cancelled (the refresh + attach goroutines
// must observe cancellation and exit — no hang).
func TestRun_StartsAndStopsCleanly(t *testing.T) {
	f := newFakeController()
	f.plans = []store.Plan{{ID: "p1", Title: "Ship It", Status: store.PlanStatusActive}}
	f.status = ipc.StatusReply{ActivePlans: 1, ReadyTasks: 2}

	ctx, cancel := context.WithCancel(context.Background())
	a := test.NewApp()
	t.Cleanup(a.Quit)
	painted := make(chan struct{}, 1)

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, Opts{Controller: f, ProjectID: "proj", fyneApp: a, painted: painted})
	}()

	// test.NewApp creates a dummy window before Run starts, so AllWindows()>0
	// does not prove our UI opened or painted. Wait for the explicit post-render
	// signal from Run's actual initial frame.
	//
	// The budget is deliberately far larger than the work. This test asserts
	// "does it EVER paint", not "does it paint within N" -- there is no product
	// requirement that the first frame land in 3s, and a headless CI runner is
	// slower and noisier than any dev machine. The former 3s deadline failed on
	// ubuntu-latest while passing 3/3 locally, which is the tell: the threshold
	// was measuring runner speed rather than correctness.
	//
	// A hang still fails, just later -- and a test that takes 30s to report a
	// real hang is strictly better than one that reports a phantom one on a busy
	// runner. See AGENTS.md on thresholds that measure the machine.
	select {
	case <-painted:
	case <-time.After(guiWaitBudget):
		cancel()
		t.Fatal("GUI initial frame never rendered within guiWaitBudget; Run is hung, not merely slow")
	}

	// Cancel and confirm Run returns (its goroutines exit on ctx.Done).
	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(guiWaitBudget):
		t.Fatal("Run did not return within 3s of context cancel")
	}
}

func TestLifecycleGroup_ClosesAdmissionAndWaitsForRunningWork(t *testing.T) {
	var group lifecycleGroup
	started := make(chan struct{})
	release := make(chan struct{})
	if !group.Go(func() {
		close(started)
		<-release
	}) {
		t.Fatal("open lifecycle group rejected work")
	}
	<-started

	group.Close()
	if group.Go(func() { t.Error("closed lifecycle group ran new work") }) {
		t.Fatal("closed lifecycle group admitted new work")
	}

	waited := make(chan struct{})
	go func() {
		group.Wait()
		close(waited)
	}()
	select {
	case <-waited:
		close(release)
		t.Fatal("lifecycle group returned before running work completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-waited:
	case <-time.After(guiWaitBudget):
		t.Fatal("lifecycle group did not finish after running work completed")
	}
}

// TestRefreshNow_SerializesPaints reproduces the headless-driver half of the
// Linux crash: concurrent refresh sources dispatch from different goroutines,
// and the test driver executes both callbacks inline. Gathering may overlap,
// but widget layout must never have more than one paint in flight because
// Fyne's package-global HarfBuzz shaper reuses a mutable buffer.
func TestRefreshNow_SerializesPaints(t *testing.T) {
	u := newTestUI(t, newFakeController())
	u.syncRender = false

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	var attempts atomic.Int32
	firstEntered := make(chan struct{})
	bothAttempted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var first sync.Once
	u.paintAttemptHook = func() {
		if attempts.Add(1) == 2 {
			close(bothAttempted)
		}
	}
	u.paintHook = func(enter bool) {
		if !enter {
			inFlight.Add(-1)
			return
		}
		current := inFlight.Add(1)
		for {
			maximum := maxInFlight.Load()
			if current <= maximum || maxInFlight.CompareAndSwap(maximum, current) {
				break
			}
		}
		first.Do(func() {
			close(firstEntered)
			<-releaseFirst
		})
	}

	firstDone := make(chan struct{})
	go func() {
		u.refreshNow()
		close(firstDone)
	}()
	select {
	case <-firstEntered:
	case <-time.After(guiWaitBudget):
		close(releaseFirst)
		t.Fatal("first refresh never entered paint")
	}

	secondDone := make(chan struct{})
	go func() {
		u.refreshNow()
		close(secondDone)
	}()

	// Wait until both callbacks have reached the instruction immediately before
	// paintMu. The first callback is blocked in paintHook while owning the lock;
	// the second is therefore deterministically waiting to acquire it.
	select {
	case <-bothAttempted:
	case <-time.After(guiWaitBudget):
		close(releaseFirst)
		t.Fatal("both refreshes did not attempt to paint")
	}
	if u.paintMu.TryLock() {
		u.paintMu.Unlock()
		t.Error("paint mutex was not held while the first paint was in flight")
	}
	if got := maxInFlight.Load(); got != 1 {
		t.Errorf("simultaneous paints before release = %d, want 1", got)
	}
	close(releaseFirst)
	for name, done := range map[string]<-chan struct{}{
		"first":  firstDone,
		"second": secondDone,
	} {
		select {
		case <-done:
		case <-time.After(guiWaitBudget):
			t.Fatalf("%s refresh did not finish", name)
		}
	}
	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("maximum simultaneous paints = %d, want 1", got)
	}
}

func TestPaintPump_WaitsForLiveRunLoopTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queued := make(chan struct{}, 1)
	pump := newPaintPump(queued)
	var painted atomic.Bool
	dispatched := make(chan bool, 1)
	go func() {
		dispatched <- pump.Dispatch(ctx, func() { painted.Store(true) })
	}()

	select {
	case <-queued:
	case <-time.After(guiWaitBudget):
		t.Fatal("paint was not queued")
	}
	select {
	case <-dispatched:
		t.Fatal("paint dispatch completed before a live run-loop tick")
	default:
	}

	pump.Tick()
	select {
	case acknowledged := <-dispatched:
		if !acknowledged {
			t.Fatal("run-loop paint was not acknowledged")
		}
	case <-time.After(guiWaitBudget):
		t.Fatal("paint dispatch did not complete after run-loop tick")
	}
	if !painted.Load() {
		t.Fatal("run-loop tick did not execute paint")
	}

	painted.Store(false)
	go func() {
		dispatched <- pump.Dispatch(ctx, func() { painted.Store(true) })
	}()
	select {
	case <-queued:
	case <-time.After(guiWaitBudget):
		t.Fatal("second paint was not queued")
	}
	pump.Stop()
	select {
	case acknowledged := <-dispatched:
		if acknowledged {
			t.Fatal("stopped pump reported an unexecuted paint as completed")
		}
	case <-time.After(guiWaitBudget):
		t.Fatal("stopped pump did not release queued paint")
	}
	if painted.Load() {
		t.Fatal("stopped pump executed queued paint")
	}
}

func TestRun_PostDrainTrayActionsRetireWithoutFyneCalls(t *testing.T) {
	for _, tc := range []struct {
		name      string
		menuIndex int
	}{
		{name: "open", menuIndex: 0},
		{name: "quit", menuIndex: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTrayLifecycleHarness(t)
			h.drain(t)

			// This is the exact dangerous Fyne v2.8 window: the driver queue is
			// drained, but ShowAndRun is still withholding its return. Fyne may
			// invoke this systray action inline on the ClickedCh goroutine.
			h.menu.Items[tc.menuIndex].Action()
			if got := h.quitCount.Load(); got != 0 {
				t.Fatalf("post-drain App.Quit calls = %d, want 0", got)
			}
			if got := h.showCount.Load(); got != 0 {
				t.Fatalf("post-drain Window.Show calls = %d, want 0", got)
			}

			h.release()
			h.wait(t)
			if got := h.quitCount.Load(); got != 0 {
				t.Fatalf("retired App.Quit calls = %d, want 0", got)
			}
			if got := h.showCount.Load(); got != 0 {
				t.Fatalf("retired Window.Show calls = %d, want 0", got)
			}
		})
	}
}

func TestRun_QuitPreemptsPendingTrayOpen(t *testing.T) {
	h := newTrayLifecycleHarness(t)

	h.menu.Items[0].Action()
	for i := 0; i < 100; i++ {
		h.menu.Items[1].Action()
	}
	if got := h.quitCount.Load(); got != 0 {
		t.Fatalf("tray publisher App.Quit calls before tick = %d, want 0", got)
	}
	if got := h.showCount.Load(); got != 0 {
		t.Fatalf("tray publisher Window.Show calls before tick = %d, want 0", got)
	}

	h.tick()
	h.wait(t)
	if got := h.quitCount.Load(); got != 1 {
		t.Fatalf("App.Quit calls = %d, want exactly 1", got)
	}
	if got := h.showCount.Load(); got != 0 {
		t.Fatalf("Window.Show calls = %d, want 0 when quit shares the snapshot", got)
	}
}

func TestRun_TrayOpenRunsOnNextLiveTickAndCoalesces(t *testing.T) {
	h := newTrayLifecycleHarness(t)

	h.menu.Items[0].Action()
	if got := h.showCount.Load(); got != 0 {
		t.Fatalf("tray publisher Window.Show calls before tick = %d, want 0", got)
	}
	h.tick()
	if got := h.showCount.Load(); got != 1 {
		t.Fatalf("Window.Show calls after one live tick = %d, want 1", got)
	}

	for i := 0; i < 100; i++ {
		h.menu.Items[0].Action()
	}
	if got := h.showCount.Load(); got != 1 {
		t.Fatalf("repeated tray publishers called Window.Show before tick; calls = %d", got)
	}
	h.tick()
	if got := h.showCount.Load(); got != 2 {
		t.Fatalf("Window.Show calls after 100 coalesced clicks = %d, want 2 total", got)
	}
	h.tick()
	if got := h.showCount.Load(); got != 2 {
		t.Fatalf("empty later tick repeated Window.Show; calls = %d", got)
	}

	h.drain(t)
	h.release()
	h.wait(t)
}

// TestRun_RealLifecycleCancelsBeforeQuit forces Run through its blocking
// real-driver branch and verifies caller cancellation reaches the UI context
// before application teardown is requested.
func TestRun_RealLifecycleCancelsBeforeQuit(t *testing.T) {
	f := newLifecycleRefreshController()
	parentCtx, cancelParent := context.WithCancel(context.Background())
	a := test.NewApp()
	t.Cleanup(a.Quit)

	windowRunning := make(chan struct{})
	windowReleased := make(chan struct{})
	paintQueued := make(chan struct{}, 1)
	pump := newPaintPump(paintQueued)
	var tick func()
	painted := make(chan struct{}, 1)
	quitRequested := make(chan struct{}, 1)
	cancelVisibleAtQuit := make(chan bool, 1)
	quitRanOnTick := make(chan bool, 1)
	var insideTick atomic.Bool
	var quitCount atomic.Int32

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(parentCtx, Opts{
			Controller: f,
			fyneApp:    a,
			painted:    painted,
			paintPump:  pump,
			startPaintPump: func(runLoopTick func()) func() {
				tick = runLoopTick
				return func() {}
			},
			quitRequested: quitRequested,
			runWindow: func(fyne.Window) {
				close(windowRunning)
				<-windowReleased
			},
			quitApp: func() {
				quitCount.Add(1)
				quitRanOnTick <- insideTick.Load()
				uiCtx := <-f.statusCtx
				select {
				case <-uiCtx.Done():
					cancelVisibleAtQuit <- true
				default:
					cancelVisibleAtQuit <- false
				}
				close(windowReleased)
			},
		})
	}()

	select {
	case <-windowRunning:
	case <-time.After(guiWaitBudget):
		cancelParent()
		t.Fatal("forced real-driver window path did not start")
	}

	select {
	case <-paintQueued:
	case <-time.After(guiWaitBudget):
		cancelParent()
		t.Fatal("initial paint was not queued for the live run loop")
	}
	select {
	case <-painted:
		cancelParent()
		t.Fatal("initial frame reported painted before its callback executed")
	default:
	}
	select {
	case err := <-runErr:
		cancelParent()
		t.Fatalf("Run returned while its initial paint was still queued: %v", err)
	default:
	}

	tick()
	select {
	case <-painted:
	case <-time.After(guiWaitBudget):
		cancelParent()
		t.Fatal("executed paint callback did not acknowledge a complete frame")
	}

	cancelParent()
	select {
	case <-quitRequested:
	case <-time.After(guiWaitBudget):
		t.Fatal("external cancellation was not published to the UI loop")
	}
	select {
	case <-cancelVisibleAtQuit:
		t.Fatal("background cancellation called App.Quit before a live UI-loop tick")
	default:
	}

	insideTick.Store(true)
	tick()
	insideTick.Store(false)
	select {
	case visible := <-cancelVisibleAtQuit:
		if !visible {
			t.Fatal("quit was requested before the UI context was cancelled")
		}
	case <-time.After(guiWaitBudget):
		t.Fatal("external cancellation did not request GUI teardown")
	}
	select {
	case onTick := <-quitRanOnTick:
		if !onTick {
			t.Fatal("App.Quit did not run inside the live UI-loop callback")
		}
	case <-time.After(guiWaitBudget):
		t.Fatal("App.Quit execution was not observed")
	}
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(guiWaitBudget):
		t.Fatal("Run did not return after the forced window exited")
	}
	tick()
	if got := quitCount.Load(); got != 1 {
		t.Fatalf("App.Quit calls = %d, want exactly 1", got)
	}
}

// TestRun_DirectDriverDrainNeverPaintsOffMain covers Fyne's native/signal
// Driver.Quit path, which does not close Ralph's window and may discard
// Lifecycle.OnStopped before setting its internal drained flag. A snapshot that
// finishes in this gap must remain in Ralph's queue; it must never fall through
// Fyne's post-drain inline dispatcher behavior.
func TestRun_DirectDriverDrainNeverPaintsOffMain(t *testing.T) {
	f := &directDrainController{
		lifecycleRefreshController: newLifecycleRefreshController(),
		lateGatherStarted:          make(chan struct{}),
		lateGatherReleased:         make(chan struct{}),
	}
	parentCtx, cancelParent := context.WithCancel(context.Background())
	a := test.NewApp()
	t.Cleanup(a.Quit)

	driverRunning := make(chan struct{})
	beginDriverDrain := make(chan struct{})
	driverDrained := make(chan struct{})
	showCanReturn := make(chan struct{})
	paintQueued := make(chan struct{}, 2)
	pump := newPaintPump(paintQueued)
	var tick func()
	painted := make(chan struct{}, 1)
	quitCalled := make(chan struct{}, 1)
	quitRequested := make(chan struct{}, 1)

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(parentCtx, Opts{
			Controller: f,
			fyneApp:    a,
			painted:    painted,
			paintPump:  pump,
			startPaintPump: func(runLoopTick func()) func() {
				tick = runLoopTick
				return func() {}
			},
			quitRequested: quitRequested,
			runWindow: func(fyne.Window) {
				close(driverRunning)
				<-beginDriverDrain
				close(driverDrained)
				<-showCanReturn
			},
			quitApp: func() {
				quitCalled <- struct{}{}
			},
		})
	}()

	select {
	case <-driverRunning:
	case <-time.After(guiWaitBudget):
		t.Fatal("forced driver did not start")
	}
	select {
	case <-paintQueued:
	case <-time.After(guiWaitBudget):
		t.Fatal("initial paint was not queued")
	}
	tick()
	select {
	case <-painted:
	case <-time.After(guiWaitBudget):
		t.Fatal("live driver tick did not paint the initial frame")
	}
	var uiCtx context.Context
	select {
	case uiCtx = <-f.statusCtx:
	case <-time.After(guiWaitBudget):
		t.Fatal("initial gather did not expose its UI context")
	}

	f.refreshEvent <- struct{}{}
	select {
	case <-f.lateGatherStarted:
	case <-time.After(guiWaitBudget):
		t.Fatal("event refresh did not enter its blocked gather")
	}

	// Simulate Fyne catchTerm/native Driver.Quit completing its internal queue
	// drain while ShowAndRun is still unwinding. Ralph has received neither
	// caller cancellation nor Window.OnClosed, so uiCtx is deliberately live.
	close(beginDriverDrain)
	select {
	case <-driverDrained:
	case <-time.After(guiWaitBudget):
		t.Fatal("forced driver did not reach its internally-drained state")
	}
	select {
	case <-uiCtx.Done():
		t.Fatal("test did not reproduce the live-ui-context post-drain gap")
	default:
	}
	cancelParent()
	select {
	case <-quitRequested:
	case <-time.After(guiWaitBudget):
		t.Fatal("post-drain parent cancellation was not published")
	}
	select {
	case <-quitCalled:
		t.Fatal("post-drain parent cancellation called App.Quit without a UI-loop tick")
	default:
	}
	select {
	case <-uiCtx.Done():
		t.Fatal("post-drain request bypassed ordered pump-stop/cancel shutdown")
	default:
	}
	close(f.lateGatherReleased)
	select {
	case <-paintQueued:
	case <-time.After(guiWaitBudget):
		t.Fatal("post-drain gather did not enqueue into Ralph's paint pump")
	}
	select {
	case <-painted:
		t.Fatal("post-drain gather painted without a live run-loop tick")
	default:
	}

	// ShowAndRun can now return. Run must synchronously stop the pump, cancel the
	// queued waiter, and join all Ralph-owned work.
	close(showCanReturn)
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(guiWaitBudget):
		t.Fatal("Run did not cancel the queued paint after direct driver drain")
	}
	select {
	case <-quitCalled:
		t.Fatal("natural direct-driver exit redundantly called App.Quit")
	default:
	}

	// Even an erroneous late animation tick sees a stopped, drained pump.
	tick()
	select {
	case <-painted:
		t.Fatal("queued callback painted after direct driver drain")
	default:
	}
}

// TestRun_ParentTickWinnerDoesNotBlockConcurrentTrayQuit is the deterministic
// admission regression: a live-loop tick owns a deliberately-blocked App.Quit
// after consuming the parent request. A concurrent losing tray action must
// return immediately.
func TestRun_ParentTickWinnerDoesNotBlockConcurrentTrayQuit(t *testing.T) {
	f := newFakeController()
	parentCtx, cancelParent := context.WithCancel(context.Background())
	baseApp := test.NewApp()
	t.Cleanup(baseApp.Quit)
	a := &capturingDesktopApp{App: baseApp, menuSet: make(chan *fyne.Menu, 1)}

	windowRunning := make(chan struct{})
	windowReleased := make(chan struct{})
	quitEntered := make(chan struct{})
	releaseQuit := make(chan struct{})
	pump := newPaintPump(nil)
	quitRequested := make(chan struct{}, 1)
	var tick func()

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(parentCtx, Opts{
			Controller: f,
			fyneApp:    a,
			paintPump:  pump,
			startPaintPump: func(runLoopTick func()) func() {
				tick = runLoopTick
				return func() {}
			},
			quitRequested: quitRequested,
			runWindow: func(fyne.Window) {
				close(windowRunning)
				<-windowReleased
			},
			quitApp: func() {
				close(quitEntered)
				<-releaseQuit
				close(windowReleased)
			},
			localize: func(label string) string {
				if label == "Quit" {
					return "Beenden"
				}
				return label
			},
		})
	}()

	var menu *fyne.Menu
	select {
	case menu = <-a.menuSet:
	case <-time.After(guiWaitBudget):
		cancelParent()
		t.Fatal("desktop app did not receive a system tray menu")
	}
	select {
	case <-windowRunning:
	case <-time.After(guiWaitBudget):
		cancelParent()
		t.Fatal("forced real-driver window path did not start")
	}
	if len(menu.Items) != 2 {
		t.Fatalf("tray menu item count = %d, want 2 without appended driver Quit", len(menu.Items))
	}
	quitItem := menu.Items[1]
	if !quitItem.IsQuit {
		t.Fatal("localized tray quit item was not marked IsQuit")
	}
	if quitItem.Label != "Beenden" {
		t.Fatalf("localized tray quit label = %q, want Beenden", quitItem.Label)
	}

	cancelParent()
	select {
	case <-quitRequested:
	case <-time.After(guiWaitBudget):
		t.Fatal("parent-cancel watcher did not publish a UI-loop quit request")
	}
	select {
	case <-quitEntered:
		t.Fatal("parent-cancel watcher called App.Quit directly")
	default:
	}

	tickDone := make(chan struct{})
	go func() {
		tick()
		close(tickDone)
	}()
	select {
	case <-quitEntered:
	case <-time.After(guiWaitBudget):
		t.Fatal("UI-loop tick did not consume the parent quit request")
	}

	trayReturned := make(chan struct{})
	go func() {
		quitItem.Action()
		close(trayReturned)
	}()
	select {
	case <-trayReturned:
	case <-time.After(250 * time.Millisecond):
		close(releaseQuit)
		t.Fatal("losing tray quit action blocked behind the tick-owned App.Quit")
	}

	close(releaseQuit)
	select {
	case <-tickDone:
	case <-time.After(guiWaitBudget):
		t.Fatal("winning UI-loop quit did not return")
	}
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(guiWaitBudget):
		t.Fatal("Run did not return after App.Quit completed")
	}
}

// TestRun_TrayRequestWinnerDoesNotAdmitLaterParentQuit covers the inverse
// publication race: the tray callback publishes and returns without touching
// Fyne, then the next live tick owns App.Quit. Later parent cancellation cannot
// publish or execute a second quit.
func TestRun_TrayRequestWinnerDoesNotAdmitLaterParentQuit(t *testing.T) {
	f := newFakeController()
	parentCtx, cancelParent := context.WithCancel(context.Background())
	baseApp := test.NewApp()
	t.Cleanup(baseApp.Quit)
	a := &capturingDesktopApp{App: baseApp, menuSet: make(chan *fyne.Menu, 1)}

	windowRunning := make(chan struct{})
	windowReleased := make(chan struct{})
	quitEntered := make(chan struct{}, 2)
	releaseQuit := make(chan struct{})
	quitRequested := make(chan struct{}, 1)
	var tick func()
	var quitCount atomic.Int32

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(parentCtx, Opts{
			Controller: f,
			fyneApp:    a,
			paintPump:  newPaintPump(nil),
			startPaintPump: func(runLoopTick func()) func() {
				tick = runLoopTick
				return func() {}
			},
			quitRequested: quitRequested,
			runWindow: func(fyne.Window) {
				close(windowRunning)
				<-windowReleased
			},
			quitApp: func() {
				quitCount.Add(1)
				quitEntered <- struct{}{}
				<-releaseQuit
				close(windowReleased)
			},
		})
	}()

	var menu *fyne.Menu
	select {
	case menu = <-a.menuSet:
	case <-time.After(guiWaitBudget):
		cancelParent()
		t.Fatal("desktop app did not receive a system tray menu")
	}
	select {
	case <-windowRunning:
	case <-time.After(guiWaitBudget):
		cancelParent()
		t.Fatal("forced real-driver window path did not start")
	}

	trayDone := make(chan struct{})
	go func() {
		menu.Items[1].Action()
		close(trayDone)
	}()
	select {
	case <-trayDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("tray quit publisher blocked")
	}
	select {
	case <-quitRequested:
	case <-time.After(guiWaitBudget):
		t.Fatal("tray callback did not publish a quit request")
	}
	select {
	case <-quitEntered:
		t.Fatal("tray callback called App.Quit directly")
	default:
	}

	tickDone := make(chan struct{})
	go func() {
		tick()
		close(tickDone)
	}()
	select {
	case <-quitEntered:
	case <-time.After(guiWaitBudget):
		close(releaseQuit)
		t.Fatal("live UI-loop tick did not consume the tray quit request")
	}

	cancelParent()
	select {
	case <-quitRequested:
		t.Fatal("parent watcher published a second request after tray quit admission")
	default:
	}
	select {
	case <-quitEntered:
		t.Fatal("parent cancellation executed a second App.Quit")
	default:
	}

	close(releaseQuit)
	select {
	case <-tickDone:
	case <-time.After(guiWaitBudget):
		t.Fatal("winning UI-loop quit did not return")
	}
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(guiWaitBudget):
		t.Fatal("Run did not return after tray App.Quit completed")
	}
	select {
	case <-quitRequested:
		t.Fatal("joined parent watcher published a duplicate quit request")
	default:
	}
	if got := quitCount.Load(); got != 1 {
		t.Fatalf("App.Quit calls = %d, want exactly 1", got)
	}
}

// TestRun_WaitsForInitialRefreshBeforeReturn guards the lifecycle edge that
// caused the Linux HarfBuzz panic. Fyne's headless driver runs UI dispatches
// inline on the calling goroutine, while its text shaper is process-global and
// not concurrency-safe. Run must therefore join its initial refresh before the
// test app can be torn down and a later test creates another Fyne app.
func TestRun_WaitsForInitialRefreshBeforeReturn(t *testing.T) {
	f := &blockingStatusController{
		fakeController: newFakeController(),
		statusStarted:  make(chan struct{}),
		statusRelease:  make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := test.NewApp()
	t.Cleanup(a.Quit)

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, Opts{Controller: f, ProjectID: "proj", fyneApp: a})
	}()

	select {
	case <-f.statusStarted:
	case <-time.After(guiWaitBudget):
		cancel()
		close(f.statusRelease)
		t.Fatal("initial GUI refresh never reached Status")
	}

	cancel()
	select {
	case err := <-runErr:
		close(f.statusRelease)
		t.Fatalf("Run returned before its initial refresh completed: %v", err)
	case <-time.After(50 * time.Millisecond):
		// Expected: shutdown is waiting for the in-flight refresh.
	}

	close(f.statusRelease)
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(guiWaitBudget):
		t.Fatal("Run did not return after its initial refresh completed")
	}
}

func TestWaitForShowExitOrCancellation_NaturalExitDoesNotQuit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	showExited := make(chan struct{})
	close(showExited)

	for i := 0; i < 1000; i++ {
		quitCalled := false
		waitForShowExitOrCancellation(ctx, showExited, func() { quitCalled = true })
		if quitCalled {
			t.Fatalf("iteration %d: natural ShowAndRun exit lost when both channels were ready", i)
		}
	}
}

func TestWaitForShowExitOrCancellation_CancellationQuits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	showExited := make(chan struct{})
	quitCalled := false

	waitForShowExitOrCancellation(ctx, showExited, func() { quitCalled = true })

	if !quitCalled {
		t.Fatal("caller cancellation did not request GUI shutdown")
	}
}

// TestRun_RequiresController confirms Run rejects a nil controller rather than
// panicking on first use.
func TestRun_RequiresController(t *testing.T) {
	if err := Run(context.Background(), Opts{}); err == nil {
		t.Error("Run with nil Controller: want error, got nil")
	}
}

// TestEventTriggersRefresh confirms the runAttach refresh gate: lifecycle events
// trigger a refresh, while pure log/heartbeat kinds are skipped.
func TestEventTriggersRefresh(t *testing.T) {
	cases := []struct {
		kind string
		want bool
	}{
		{"task.done", true},
		{"task.claimed", true},
		{"plan.imported", true},
		{"worker.completed", true},
		{"tick", false},
		{"task.progress", false},
		{"", true},
	}
	for _, tc := range cases {
		if got := eventTriggersRefresh(ipc.AttachEvent{Kind: tc.kind}); got != tc.want {
			t.Errorf("eventTriggersRefresh(%q) = %v, want %v", tc.kind, got, tc.want)
		}
	}
}

// TestRunAttach_ReconnectsAfterStreamEnds is the regression for the GUI audit's
// C1: the live attach subscription was single-shot — Attach returning (a failed
// pre-supervisor dial, or an EOF when the supervisor restarts) killed the stream
// permanently for the rest of the session. runAttach must re-dial in a loop, so
// the event stream recovers after a supervisor blip.
func TestRunAttach_ReconnectsAfterStreamEnds(t *testing.T) {
	f := newFakeController()
	// Attach returns immediately every call — as if the supervisor is down / the
	// stream keeps ending. runAttach must keep re-dialing.
	f.attachReturn = context.Canceled

	u := newTestUI(t, f)
	// Shrink the retry delay on THIS ui only (per-instance field — no shared
	// global to race another test's runAttach goroutine).
	u.attachRetryDelay = 1 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { u.runAttach(ctx); close(done) }()

	// Poll until Attach has been called several times (proving the re-dial loop),
	// then cancel and confirm runAttach returns.
	deadline := time.After(guiWaitBudget)
	for f.attachCount.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("Attach called only %d time(s) — runAttach did not reconnect after the stream ended", f.attachCount.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(guiWaitBudget):
		t.Fatal("runAttach did not return after ctx cancel")
	}
}
