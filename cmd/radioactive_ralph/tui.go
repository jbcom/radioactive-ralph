package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/plandag"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
)

const (
	tabOverview = iota
	tabPlans
	tabReady
	tabApprovals
	tabBlocked
	tabRunning
	tabFailed
	tabEvents
)

var tabNames = []string{"overview", "plans", "ready", "approvals", "blocked", "running", "failed", "events"}

type TUICmd struct {
	RepoRoot    string `help:"Repo root. Defaults to cwd." type:"path"`
	NoAutostart bool   `help:"Do not auto-launch the repo service if it is not already running." name:"no-autostart"`
}

func (c *TUICmd) Run(_ *runContext) error {
	repo, err := resolveRepoRoot(c.RepoRoot)
	if err != nil {
		return err
	}
	socket, heartbeat, err := socketPath(repo)
	if err != nil {
		return err
	}
	started, logPath, err := ensureServiceForTUI(repo, socket, heartbeat, c.NoAutostart)
	if err != nil {
		return err
	}

	model := newTUIModel(repo, socket, heartbeat, started, logPath)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

type statusMsg struct {
	status ipc.StatusReply
	err    error
}

type eventMsg struct {
	line string
}

type serviceMsg struct {
	text string
}

type snapshotMsg struct {
	snapshot queueSnapshot
	err      error
}

type inputMode int

const (
	inputNone inputMode = iota
	inputHandoff
)

type planSummary struct {
	ID            string
	Slug          string
	Title         string
	Status        plandag.PlanStatus
	ReadyCount    int
	ApprovalCount int
	BlockCount    int
	RunningCount  int
	FailedCount   int
}

type taskSummary struct {
	PlanID            string
	PlanSlug          string
	PlanTitle         string
	TaskID            string
	Description       string
	VariantHint       string
	AssignedTo        string
	Provider          string
	ProviderSessionID string
	ClaimedBySession  string
	Status            plandag.TaskStatus
	LatestEventType   string
	LatestPayload     plandag.TaskEventPayload
	AcceptanceJSON    string
	DependsOn         []string
	DependedBy        []string
}

type queueSnapshot struct {
	plans     []planSummary
	approvals []taskSummary
	blocked   []taskSummary
	running   []taskSummary
	ready     []taskSummary
	failed    []taskSummary
	planTasks map[string][]taskSummary
}

type tuiModel struct {
	repo           string
	socket         string
	heartbeat      string
	serviceStarted bool
	serviceLog     string
	serviceRunning bool

	status   ipc.StatusReply
	snapshot queueSnapshot
	err      error
	events   []string

	statusCh   chan statusMsg
	eventCh    chan eventMsg
	snapshotCh chan snapshotMsg
	cancel     context.CancelFunc

	tab            int
	cursors        map[int]int
	selectedPlanID string
	variantFilter  string
	providerFilter string
	inputMode      inputMode
	input          string
	showHelp       bool
}

func newTUIModel(repo, socket, heartbeat string, serviceStarted bool, serviceLog string) *tuiModel {
	return &tuiModel{
		repo:           repo,
		socket:         socket,
		heartbeat:      heartbeat,
		serviceStarted: serviceStarted,
		serviceLog:     serviceLog,
		statusCh:       make(chan statusMsg),
		eventCh:        make(chan eventMsg),
		snapshotCh:     make(chan snapshotMsg),
		cursors:        map[int]int{},
	}
}

func (m *tuiModel) Init() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.pollStatusLoop(ctx)
	go m.attachLoop(ctx)
	go m.pollSnapshotLoop(ctx)
	return tea.Batch(waitStatus(m.statusCh), waitEvent(m.eventCh), waitSnapshot(m.snapshotCh))
}

func waitStatus(ch <-chan statusMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func waitEvent(ch <-chan eventMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func waitSnapshot(ch <-chan snapshotMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.inputMode != inputNone {
			return m.updateInput(msg)
		}
		return m.updateNormal(msg)
	case statusMsg:
		m.err = msg.err
		m.serviceRunning = msg.err == nil
		if msg.err == nil {
			m.status = msg.status
		}
		return m, waitStatus(m.statusCh)
	case eventMsg:
		if msg.line != "" {
			m.events = append([]string{msg.line}, m.events...)
			if len(m.events) > 20 {
				m.events = m.events[:20]
			}
		}
		return m, waitEvent(m.eventCh)
	case snapshotMsg:
		if msg.err == nil {
			m.snapshot = msg.snapshot
			if m.selectedPlanID == "" && len(msg.snapshot.plans) > 0 {
				m.selectedPlanID = msg.snapshot.plans[0].ID
			}
		}
		return m, waitSnapshot(m.snapshotCh)
	case serviceMsg:
		if msg.text != "" {
			m.events = append([]string{msg.text}, m.events...)
			if len(m.events) > 20 {
				m.events = m.events[:20]
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *tuiModel) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "tab", "l", "right":
		m.tab = (m.tab + 1) % len(tabNames)
		return m, nil
	case "shift+tab", "left":
		m.tab = (m.tab + len(tabNames) - 1) % len(tabNames)
		return m, nil
	case "j", "down":
		m.moveCursor(1)
		return m, nil
	case "k", "up":
		m.moveCursor(-1)
		return m, nil
	case "enter":
		if m.tab == tabPlans {
			if plan := m.currentPlan(); plan != nil {
				m.selectedPlanID = plan.ID
			}
		}
		return m, nil
	case "v":
		m.cycleVariantFilter()
		return m, nil
	case "p":
		m.cycleProviderFilter()
		return m, nil
	case "c":
		m.variantFilter = ""
		m.providerFilter = ""
		return m, nil
	case "r":
		return m, manualRefreshCmd(m.socket)
	case "s":
		return m, stopServiceCmd(m.socket)
	case "S":
		return m, startServiceCmd(m.repo, m.socket, m.heartbeat)
	case "a":
		if task := m.currentTask(); task != nil && m.tab == tabApprovals {
			return m, approveTaskCmd(m.repo, *task)
		}
	case "R":
		if task := m.currentTask(); task != nil {
			return m, requeueTaskCmd(m.repo, *task)
		}
	case "t":
		if task := m.currentTask(); task != nil {
			return m, retryTaskCmd(m.repo, *task)
		}
	case "f":
		if task := m.currentTask(); task != nil {
			return m, failTaskCmd(m.repo, *task)
		}
	case "d":
		if task := m.currentTask(); task != nil {
			return m, markDoneTaskCmd(m.repo, *task)
		}
	case "h":
		if task := m.currentTask(); task != nil {
			m.inputMode = inputHandoff
			m.input = ""
		}
		return m, nil
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	}
	return m, nil
}

func (m *tuiModel) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.inputMode = inputNone
		m.input = ""
		return m, nil
	case "enter":
		task := m.currentTask()
		input := strings.TrimSpace(m.input)
		mode := m.inputMode
		m.inputMode = inputNone
		m.input = ""
		if task == nil {
			return m, nil
		}
		switch mode {
		case inputHandoff:
			return m, handoffTaskCmd(m.repo, *task, input)
		default:
			return m, nil
		}
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.input += msg.String()
		}
		return m, nil
	}
}

func (m *tuiModel) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Render("radioactive_ralph cockpit")
	label := lipgloss.NewStyle().Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	activeTab := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))

	statusLine := muted.Render("service connected")
	if m.err != nil {
		statusLine = errStyle.Render(m.err.Error())
	}
	if !m.serviceRunning {
		statusLine = errStyle.Render("repo service disconnected")
	}
	filterLine := m.filterLine(muted)

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(statusLine)
	if filterLine != "" {
		b.WriteString("\n")
		b.WriteString(filterLine)
	}
	if m.serviceStarted && m.serviceLog != "" {
		b.WriteString("\n")
		b.WriteString(muted.Render("autostarted service log: " + m.serviceLog))
	}
	b.WriteString("\n\n")
	b.WriteString(renderTabs(m.tab, activeTab, muted))
	b.WriteString("\n\n")

	switch m.tab {
	case tabOverview:
		b.WriteString(m.renderOverview(label, muted))
	case tabPlans:
		b.WriteString(m.renderPlans(label, muted))
	case tabReady:
		b.WriteString(m.renderTaskList("Ready Tasks", m.filteredTasks(m.snapshot.ready), label, muted))
	case tabApprovals:
		b.WriteString(m.renderTaskList("Waiting Approval", m.filteredTasks(m.snapshot.approvals), label, muted))
	case tabBlocked:
		b.WriteString(m.renderTaskList("Blocked Tasks", m.filteredTasks(m.snapshot.blocked), label, muted))
	case tabRunning:
		b.WriteString(m.renderRunning(label, muted))
	case tabFailed:
		b.WriteString(m.renderTaskList("Failed Tasks", m.filteredTasks(m.snapshot.failed), label, muted))
	case tabEvents:
		b.WriteString(m.renderEvents(label, muted))
	}

	b.WriteString("\n")
	if m.inputMode == inputHandoff {
		b.WriteString(label.Render("handoff input"))
		b.WriteString("\n")
		b.WriteString(muted.Render("enter variant[:reason], press Enter to submit or Esc to cancel"))
		b.WriteString("\n> ")
		b.WriteString(m.input)
		b.WriteString("\n\n")
	}
	if m.showHelp {
		b.WriteString(renderHelpOverlay(label, muted))
	} else {
		b.WriteString(muted.Render("keys: tab/←→ switch • ↑↓ move • v/p filters • c clear • r refresh • s stop • S start • a approve • R requeue • t retry • h handoff • d done • f fail • ? help • q quit"))
	}
	return b.String()
}

// renderHelpOverlay is the verbose help panel toggled with `?`. It
// groups shortcuts by category so operators can discover actions
// without reading source.
func renderHelpOverlay(label, muted lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(label.Render("Keyboard shortcuts"))
	b.WriteString("\n\n")
	b.WriteString(label.Render("Navigation"))
	b.WriteString("\n")
	b.WriteString("  tab / →             next tab\n")
	b.WriteString("  shift+tab / ←       previous tab\n")
	b.WriteString("  j / ↓               cursor down\n")
	b.WriteString("  k / ↑               cursor up\n")
	b.WriteString("  enter               select plan (on Plans tab)\n\n")
	b.WriteString(label.Render("Filters"))
	b.WriteString("\n")
	b.WriteString("  v                   cycle variant filter\n")
	b.WriteString("  p                   cycle provider filter\n")
	b.WriteString("  c                   clear filters\n\n")
	b.WriteString(label.Render("Service"))
	b.WriteString("\n")
	b.WriteString("  r                   refresh status now\n")
	b.WriteString("  s                   stop the repo service\n")
	b.WriteString("  S                   start the repo service\n\n")
	b.WriteString(label.Render("Task actions"))
	b.WriteString("\n")
	b.WriteString("  a                   approve (Approvals tab only)\n")
	b.WriteString("  R                   requeue\n")
	b.WriteString("  t                   retry\n")
	b.WriteString("  h                   handoff (prompts for variant[:reason])\n")
	b.WriteString("  d                   mark done\n")
	b.WriteString("  f                   mark failed\n\n")
	b.WriteString(label.Render("Misc"))
	b.WriteString("\n")
	b.WriteString("  ?                   toggle this overlay\n")
	b.WriteString("  q / ctrl+c          quit\n")
	b.WriteString("\n")
	b.WriteString(muted.Render("press ? again to close"))
	return b.String()
}

func (m *tuiModel) renderOverview(label, muted lipgloss.Style) string {
	metrics := []string{
		fmt.Sprintf("%s %s", label.Render("Repo:"), m.repo),
		fmt.Sprintf("%s %d", label.Render("PID:"), m.status.PID),
		fmt.Sprintf("%s %s", label.Render("Uptime:"), m.status.Uptime.Round(time.Second)),
		fmt.Sprintf("%s %d", label.Render("Active plans:"), m.status.ActivePlans),
		fmt.Sprintf("%s %d", label.Render("Ready tasks:"), m.status.ReadyTasks),
		fmt.Sprintf("%s %d", label.Render("Waiting approval:"), m.status.ApprovalTasks),
		fmt.Sprintf("%s %d", label.Render("Blocked tasks:"), m.status.BlockedTasks),
		fmt.Sprintf("%s %d", label.Render("Running tasks:"), m.status.RunningTasks),
		fmt.Sprintf("%s %d", label.Render("Failed tasks:"), m.status.FailedTasks),
		fmt.Sprintf("%s %d", label.Render("Active workers:"), m.status.ActiveWorkers),
	}

	var b strings.Builder
	for _, line := range metrics {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(label.Render("Workers"))
	b.WriteString("\n")
	if len(m.status.Workers) == 0 {
		b.WriteString(muted.Render("no active workers"))
		b.WriteString("\n")
	} else {
		for _, worker := range m.status.Workers {
			line := fmt.Sprintf("%s/%s  variant=%s", worker.PlanID, worker.TaskID, worker.Variant)
			if worker.Provider != "" {
				line += "  provider=" + worker.Provider
			}
			if worker.WorktreePath != "" {
				line += "  worktree=" + worker.WorktreePath
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(label.Render("Immediate Queues"))
	b.WriteString("\n")
	b.WriteString(renderLimitedTasks(m.snapshot.approvals, "approval", muted))
	b.WriteString(renderLimitedTasks(m.snapshot.blocked, "blocked", muted))
	b.WriteString(renderLimitedTasks(m.snapshot.running, "running", muted))
	return b.String()
}

func (m *tuiModel) renderPlans(label, muted lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(label.Render("Plans"))
	b.WriteString("\n")
	if len(m.snapshot.plans) == 0 {
		b.WriteString(muted.Render("no active or paused plans in this repo"))
		b.WriteString("\n")
		return b.String()
	}
	cursor := m.cursors[tabPlans]
	if cursor >= len(m.snapshot.plans) {
		cursor = len(m.snapshot.plans) - 1
	}
	for idx, plan := range m.snapshot.plans {
		prefix := "  "
		if idx == cursor {
			prefix = "> "
		}
		fmt.Fprintf(&b, "%s%s  [%s]  ready=%d approval=%d blocked=%d running=%d failed=%d\n",
			prefix, trunc(plan.Slug, 20), plan.Status, plan.ReadyCount, plan.ApprovalCount, plan.BlockCount, plan.RunningCount, plan.FailedCount)
	}
	selected := m.currentPlan()
	if selected == nil {
		return b.String()
	}
	m.selectedPlanID = selected.ID
	b.WriteString("\n")
	b.WriteString(label.Render("Selected Plan"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n%s\nstatus=%s\n", selected.Title, selected.Slug, selected.Status)
	tasks := m.filteredTasks(m.snapshot.planTasks[selected.ID])
	if len(tasks) == 0 {
		b.WriteString(muted.Render("no tracked tasks in the current plan snapshot"))
		b.WriteString("\n")
		return b.String()
	}
	for _, task := range tasks {
		b.WriteString(renderTaskLine(task))
		b.WriteString("\n")
	}
	return b.String()
}

func (m *tuiModel) renderTaskList(title string, tasks []taskSummary, label, muted lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(label.Render(title))
	b.WriteString("\n")
	if len(tasks) == 0 {
		b.WriteString(muted.Render("none"))
		b.WriteString("\n")
		return b.String()
	}
	cursor := m.currentCursor()
	for idx, task := range tasks {
		prefix := "  "
		if idx == cursor {
			prefix = "> "
		}
		b.WriteString(prefix)
		b.WriteString(renderTaskLine(task))
		b.WriteString("\n")
	}
	if task := m.currentTask(); task != nil {
		b.WriteString("\n")
		b.WriteString(label.Render("Selected Task"))
		b.WriteString("\n")
		b.WriteString(renderTaskDetail(*task, muted))
	}
	return b.String()
}

func (m *tuiModel) renderRunning(label, muted lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(m.renderTaskList("Running Tasks", m.filteredTasks(m.snapshot.running), label, muted))
	b.WriteString("\n")
	b.WriteString(label.Render("Worker Sessions"))
	b.WriteString("\n")
	if len(m.status.Workers) == 0 {
		b.WriteString(muted.Render("no active workers"))
		b.WriteString("\n")
		return b.String()
	}
	for _, worker := range m.status.Workers {
		line := fmt.Sprintf("%s/%s  variant=%s", worker.PlanID, worker.TaskID, worker.Variant)
		if worker.Provider != "" {
			line += "  provider=" + worker.Provider
		}
		if worker.ProviderSessionID != "" {
			line += "  session=" + worker.ProviderSessionID
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (m *tuiModel) renderEvents(label, muted lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(label.Render("Recent Events"))
	b.WriteString("\n")
	if len(m.events) == 0 {
		b.WriteString(muted.Render("waiting for repo service events..."))
		b.WriteString("\n")
		return b.String()
	}
	for _, line := range m.events {
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (m *tuiModel) moveCursor(delta int) {
	cursor := m.currentCursor() + delta
	listLen := m.currentListLength()
	if listLen <= 0 {
		m.cursors[m.tab] = 0
		return
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= listLen {
		cursor = listLen - 1
	}
	m.cursors[m.tab] = cursor
}

func (m *tuiModel) currentCursor() int {
	return m.cursors[m.tab]
}

func (m *tuiModel) currentListLength() int {
	switch m.tab {
	case tabPlans:
		return len(m.snapshot.plans)
	case tabReady:
		return len(m.filteredTasks(m.snapshot.ready))
	case tabApprovals:
		return len(m.filteredTasks(m.snapshot.approvals))
	case tabBlocked:
		return len(m.filteredTasks(m.snapshot.blocked))
	case tabRunning:
		return len(m.filteredTasks(m.snapshot.running))
	case tabFailed:
		return len(m.filteredTasks(m.snapshot.failed))
	default:
		return 0
	}
}

func (m *tuiModel) currentPlan() *planSummary {
	if len(m.snapshot.plans) == 0 {
		return nil
	}
	cursor := m.cursors[tabPlans]
	if cursor < 0 || cursor >= len(m.snapshot.plans) {
		cursor = 0
	}
	return &m.snapshot.plans[cursor]
}

func (m *tuiModel) currentTask() *taskSummary {
	var tasks []taskSummary
	switch m.tab {
	case tabReady:
		tasks = m.filteredTasks(m.snapshot.ready)
	case tabApprovals:
		tasks = m.filteredTasks(m.snapshot.approvals)
	case tabBlocked:
		tasks = m.filteredTasks(m.snapshot.blocked)
	case tabRunning:
		tasks = m.filteredTasks(m.snapshot.running)
	case tabFailed:
		tasks = m.filteredTasks(m.snapshot.failed)
	default:
		return nil
	}
	if len(tasks) == 0 {
		return nil
	}
	cursor := m.currentCursor()
	if cursor < 0 || cursor >= len(tasks) {
		cursor = 0
	}
	return &tasks[cursor]
}

func (m *tuiModel) filterLine(muted lipgloss.Style) string {
	var parts []string
	if m.variantFilter != "" {
		parts = append(parts, "variant="+m.variantFilter)
	}
	if m.providerFilter != "" {
		parts = append(parts, "provider="+m.providerFilter)
	}
	if len(parts) == 0 {
		return ""
	}
	return muted.Render("filters: " + strings.Join(parts, "  "))
}

func (m *tuiModel) cycleVariantFilter() {
	m.variantFilter = nextFilter(m.variantFilter, m.availableVariants())
	m.resetVisibleCursor()
}

func (m *tuiModel) cycleProviderFilter() {
	m.providerFilter = nextFilter(m.providerFilter, m.availableProviders())
	m.resetVisibleCursor()
}

func (m *tuiModel) resetVisibleCursor() {
	m.cursors[tabReady] = 0
	m.cursors[tabApprovals] = 0
	m.cursors[tabBlocked] = 0
	m.cursors[tabRunning] = 0
	m.cursors[tabFailed] = 0
}

func (m *tuiModel) availableVariants() []string {
	values := map[string]bool{}
	for _, task := range m.allTasks() {
		if value := taskVariantLabel(task); value != "" {
			values[value] = true
		}
	}
	for _, worker := range m.status.Workers {
		if worker.Variant != "" {
			values[worker.Variant] = true
		}
	}
	return sortedKeys(values)
}

func (m *tuiModel) availableProviders() []string {
	values := map[string]bool{}
	for _, task := range m.allTasks() {
		if value := taskProviderLabel(task); value != "" {
			values[value] = true
		}
	}
	for _, worker := range m.status.Workers {
		if worker.Provider != "" {
			values[worker.Provider] = true
		}
	}
	return sortedKeys(values)
}

func (m *tuiModel) allTasks() []taskSummary {
	out := make([]taskSummary, 0,
		len(m.snapshot.ready)+len(m.snapshot.approvals)+len(m.snapshot.blocked)+
			len(m.snapshot.running)+len(m.snapshot.failed))
	out = append(out, m.snapshot.ready...)
	out = append(out, m.snapshot.approvals...)
	out = append(out, m.snapshot.blocked...)
	out = append(out, m.snapshot.running...)
	out = append(out, m.snapshot.failed...)
	return out
}

func (m *tuiModel) filteredTasks(tasks []taskSummary) []taskSummary {
	if m.variantFilter == "" && m.providerFilter == "" {
		return tasks
	}
	out := make([]taskSummary, 0, len(tasks))
	for _, task := range tasks {
		if m.variantFilter != "" && taskVariantLabel(task) != m.variantFilter {
			continue
		}
		if m.providerFilter != "" && taskProviderLabel(task) != m.providerFilter {
			continue
		}
		out = append(out, task)
	}
	return out
}

func nextFilter(current string, values []string) string {
	if len(values) == 0 {
		return ""
	}
	cycle := append([]string{""}, values...)
	for idx, value := range cycle {
		if value == current {
			return cycle[(idx+1)%len(cycle)]
		}
	}
	return cycle[1]
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func taskVariantLabel(task taskSummary) string {
	switch {
	case task.AssignedTo != "":
		return task.AssignedTo
	case task.VariantHint != "":
		return task.VariantHint
	case task.LatestPayload.HandoffTo != "":
		return task.LatestPayload.HandoffTo
	default:
		return ""
	}
}

func taskProviderLabel(task taskSummary) string {
	if task.Provider != "" {
		return task.Provider
	}
	return task.LatestPayload.Provider
}

func renderTabs(active int, activeStyle, muted lipgloss.Style) string {
	items := make([]string, 0, len(tabNames))
	for idx, name := range tabNames {
		if idx == active {
			items = append(items, activeStyle.Render("["+name+"]"))
			continue
		}
		items = append(items, muted.Render(name))
	}
	return strings.Join(items, "  ")
}

func renderTaskLine(task taskSummary) string {
	line := fmt.Sprintf("[%s] %s/%s  %s", task.Status, trunc(task.PlanSlug, 20), task.TaskID, task.Description)
	var extras []string
	if task.VariantHint != "" {
		extras = append(extras, "hint="+task.VariantHint)
	}
	if task.AssignedTo != "" {
		extras = append(extras, "assigned="+task.AssignedTo)
	}
	if task.Provider != "" {
		extras = append(extras, "provider="+task.Provider)
	}
	if task.LatestPayload.HandoffTo != "" {
		extras = append(extras, "handoff_to="+task.LatestPayload.HandoffTo)
	}
	if len(extras) > 0 {
		line += "  (" + strings.Join(extras, ", ") + ")"
	}
	return line
}

func renderTaskDetail(task taskSummary, muted lipgloss.Style) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s/%s\n", task.PlanSlug, task.TaskID)
	b.WriteString(task.Description)
	b.WriteString("\n")
	if task.LatestEventType != "" {
		b.WriteString("latest_event=" + task.LatestEventType + "\n")
	}
	if task.Provider != "" {
		b.WriteString("provider=" + task.Provider + "\n")
	}
	if task.ProviderSessionID != "" {
		b.WriteString("provider_session_id=" + task.ProviderSessionID + "\n")
	}
	if task.LatestPayload.Reason != "" {
		b.WriteString("reason=" + task.LatestPayload.Reason + "\n")
	}
	if len(task.LatestPayload.NeedsContext) > 0 {
		b.WriteString("needs_context=" + strings.Join(task.LatestPayload.NeedsContext, ", ") + "\n")
	}
	if len(task.LatestPayload.Evidence) > 0 {
		b.WriteString("evidence:\n")
		for _, item := range task.LatestPayload.Evidence {
			b.WriteString("  - " + item + "\n")
		}
	}
	if task.ClaimedBySession != "" {
		b.WriteString(muted.Render("claimed_by_session=" + task.ClaimedBySession))
		b.WriteString("\n")
	}
	if accept := formatAcceptance(task.AcceptanceJSON); accept != "" {
		b.WriteString("acceptance:\n")
		b.WriteString(accept)
	}
	if len(task.DependsOn) > 0 {
		b.WriteString("depends_on: " + strings.Join(task.DependsOn, ", ") + "\n")
	}
	if len(task.DependedBy) > 0 {
		b.WriteString("depended_by: " + strings.Join(task.DependedBy, ", ") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatAcceptance renders a task's acceptance_json as a compact
// bullet list if it parses, or returns the raw string if it doesn't.
// Empty input → empty output.
func formatAcceptance(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var items []string
	if err := json.Unmarshal([]byte(raw), &items); err == nil && len(items) > 0 {
		var b strings.Builder
		for _, item := range items {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
		return b.String()
	}
	return "  " + raw + "\n"
}

func renderLimitedTasks(tasks []taskSummary, label string, muted lipgloss.Style) string {
	if len(tasks) == 0 {
		return muted.Render("no "+label+" tasks") + "\n"
	}
	var b strings.Builder
	limit := minInt(len(tasks), 3)
	for i := 0; i < limit; i++ {
		b.WriteString(renderTaskLine(tasks[i]))
		b.WriteString("\n")
	}
	return b.String()
}

func (m *tuiModel) pollStatusLoop(ctx context.Context) {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := pollStatusOnce(ctx, m.socket)
		select {
		case <-ctx.Done():
			return
		case m.statusCh <- statusMsg{status: status, err: err}:
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func pollStatusOnce(ctx context.Context, socket string) (ipc.StatusReply, error) {
	client, err := ipc.Dial(socket, 2*time.Second)
	if err != nil {
		return ipc.StatusReply{}, err
	}
	defer func() { _ = client.Close() }()
	statusCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return client.Status(statusCtx)
}

func (m *tuiModel) pollSnapshotLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		snapshot, err := loadQueueSnapshot(ctx, m.repo)
		select {
		case <-ctx.Done():
			return
		case m.snapshotCh <- snapshotMsg{snapshot: snapshot, err: err}:
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *tuiModel) attachLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		client, err := ipc.Dial(m.socket, 2*time.Second)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(1500 * time.Millisecond):
			}
			continue
		}
		attachErr := client.Attach(ctx, func(raw json.RawMessage) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case m.eventCh <- eventMsg{line: summarizeEvent(raw)}:
				return nil
			}
		})
		_ = client.Close()
		if attachErr != nil && attachErr != ipc.ErrClosed && ctx.Err() == nil {
			select {
			case <-ctx.Done():
				return
			case m.eventCh <- eventMsg{line: "attach disconnected: " + attachErr.Error()}:
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

func summarizeEvent(raw json.RawMessage) string {
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return string(raw)
	}
	ts, _ := parsed["timestamp"].(string)
	kind, _ := parsed["kind"].(string)
	actor, _ := parsed["actor"].(string)
	if ts == "" {
		ts = time.Now().Format(time.RFC3339)
	}
	if actor == "" {
		actor = "radioactive_ralph"
	}
	return fmt.Sprintf("%s  %-22s %s", ts, kind, actor)
}

func loadQueueSnapshot(ctx context.Context, repo string) (queueSnapshot, error) {
	store, err := openPlanStore(ctx)
	if err != nil {
		return queueSnapshot{}, err
	}
	defer func() { _ = store.Close() }()

	plans, err := store.ListPlans(ctx, []plandag.PlanStatus{
		plandag.PlanStatusActive,
		plandag.PlanStatusPaused,
	})
	if err != nil {
		return queueSnapshot{}, err
	}
	items, err := store.ListRepoTaskSummaries(ctx, repo, []plandag.TaskStatus{
		plandag.TaskStatusPending,
		plandag.TaskStatusReady,
		plandag.TaskStatusReadyPendingApproval,
		plandag.TaskStatusBlocked,
		plandag.TaskStatusRunning,
		plandag.TaskStatusFailed,
	}, 0)
	if err != nil {
		return queueSnapshot{}, err
	}

	out := queueSnapshot{
		planTasks: map[string][]taskSummary{},
	}
	counts := map[string]*planSummary{}
	for _, plan := range plans {
		if plan.RepoPath != repo {
			continue
		}
		summary := planSummary{
			ID:     plan.ID,
			Slug:   plan.Slug,
			Title:  plan.Title,
			Status: plan.Status,
		}
		out.plans = append(out.plans, summary)
		counts[plan.ID] = &out.plans[len(out.plans)-1]
	}
	for _, item := range items {
		task := toTaskSummary(item)
		// Best-effort dep enrichment — an error here doesn't kill the
		// snapshot, we just leave the drilldown dep lines blank.
		if deps, err := store.TaskDeps(ctx, item.PlanID, item.Task.ID); err == nil {
			task.DependsOn = deps.DependsOn
			task.DependedBy = deps.DependedBy
		}
		out.planTasks[item.PlanID] = appendTaskLimit(out.planTasks[item.PlanID], task, 12)
		if plan := counts[item.PlanID]; plan != nil {
			switch item.Task.Status {
			case plandag.TaskStatusPending, plandag.TaskStatusReady:
				plan.ReadyCount++
			case plandag.TaskStatusReadyPendingApproval:
				plan.ApprovalCount++
			case plandag.TaskStatusBlocked:
				plan.BlockCount++
			case plandag.TaskStatusRunning:
				plan.RunningCount++
			case plandag.TaskStatusFailed:
				plan.FailedCount++
			}
		}
		switch item.Task.Status {
		case plandag.TaskStatusReadyPendingApproval:
			out.approvals = append(out.approvals, task)
		case plandag.TaskStatusBlocked:
			out.blocked = append(out.blocked, task)
		case plandag.TaskStatusRunning:
			out.running = append(out.running, task)
		case plandag.TaskStatusPending, plandag.TaskStatusReady:
			out.ready = append(out.ready, task)
		case plandag.TaskStatusFailed:
			out.failed = append(out.failed, task)
		}
	}
	return out, nil
}

func toTaskSummary(item plandag.RepoTaskSummary) taskSummary {
	payload := parseTaskPayload(item.LatestPayloadJSON)
	return taskSummary{
		PlanID:            item.PlanID,
		PlanSlug:          item.PlanSlug,
		PlanTitle:         item.PlanTitle,
		TaskID:            item.Task.ID,
		Description:       item.Task.Description,
		VariantHint:       item.Task.VariantHint,
		AssignedTo:        item.Task.AssignedVariant,
		Provider:          payload.Provider,
		ProviderSessionID: payload.ProviderSessionID,
		ClaimedBySession:  item.Task.ClaimedBySession,
		Status:            item.Task.Status,
		LatestEventType:   item.LatestEventType,
		LatestPayload:     payload,
		AcceptanceJSON:    item.Task.AcceptanceJSON,
	}
}

func parseTaskPayload(raw string) plandag.TaskEventPayload {
	payload, err := plandag.ParseTaskPayload(raw)
	if err != nil {
		return plandag.TaskEventPayload{}
	}
	return payload
}

func appendTaskLimit(dst []taskSummary, item taskSummary, limit int) []taskSummary {
	if len(dst) >= limit {
		return dst
	}
	return append(dst, item)
}

func ensureServiceForTUI(repo, socket, heartbeat string, noAutostart bool) (bool, string, error) {
	if err := ensureAlive(socket, heartbeat); err == nil {
		return false, "", nil
	}
	if _, err := os.Stat(socket); err == nil {
		_ = os.Remove(socket)
	}
	_ = os.Remove(heartbeat)
	if noAutostart {
		return false, "", fmt.Errorf("repo service is not running; start it with `radioactive_ralph service start`")
	}

	paths, err := xdg.Resolve(repo)
	if err != nil {
		return false, "", err
	}
	if err := paths.Ensure(); err != nil {
		return false, "", err
	}
	logPath := filepath.Join(paths.Logs, "service.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // repo-local service log path
	if err != nil {
		return false, "", err
	}
	exe, err := os.Executable()
	if err != nil {
		_ = logFile.Close()
		return false, "", err
	}
	cmd := exec.Command(exe, "service", "start", "--repo-root", repo) //nolint:gosec // args are internal
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "RALPH_SERVICE_CONTEXT=1")
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return false, "", err
	}
	go func() { _, _ = cmd.Process.Wait() }()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if err := ensureAlive(socket, heartbeat); err == nil {
			_ = logFile.Close()
			return true, logPath, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = logFile.Close()
	return false, logPath, fmt.Errorf("timed out waiting for repo service to start; check %s", logPath)
}

func manualRefreshCmd(socket string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		client, err := ipc.Dial(socket, 2*time.Second)
		if err != nil {
			return serviceMsg{text: "refresh failed: " + err.Error()}
		}
		defer func() { _ = client.Close() }()
		if _, err := client.Status(ctx); err != nil {
			return serviceMsg{text: "refresh failed: " + err.Error()}
		}
		return serviceMsg{text: "refreshed"}
	}
}

func stopServiceCmd(socket string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client, err := ipc.Dial(socket, 2*time.Second)
		if err != nil {
			return serviceMsg{text: fmt.Sprintf("stop failed: %v", err)}
		}
		defer func() { _ = client.Close() }()
		if err := client.Stop(ctx, ipc.StopArgs{Graceful: true}); err != nil {
			return serviceMsg{text: fmt.Sprintf("stop failed: %v", err)}
		}
		return serviceMsg{text: "stop requested"}
	}
}

func startServiceCmd(repo, socket, heartbeat string) tea.Cmd {
	return func() tea.Msg {
		started, logPath, err := ensureServiceForTUI(repo, socket, heartbeat, false)
		if err != nil {
			return serviceMsg{text: "start failed: " + err.Error()}
		}
		if started {
			return serviceMsg{text: "service started (" + logPath + ")"}
		}
		return serviceMsg{text: "service already running"}
	}
}

func approveTaskCmd(repo string, task taskSummary) tea.Cmd {
	return func() tea.Msg {
		return serviceMsg{text: performPlanMutation(repo, func(ctx context.Context, store *plandag.Store) error {
			return store.ApproveTaskWithPayload(ctx, task.PlanID, task.TaskID, plandag.TaskEventPayload{OperatorAction: "approved"})
		}, fmt.Sprintf("approved %s/%s", task.PlanSlug, task.TaskID))}
	}
}

func requeueTaskCmd(repo string, task taskSummary) tea.Cmd {
	return func() tea.Msg {
		return serviceMsg{text: performPlanMutation(repo, func(ctx context.Context, store *plandag.Store) error {
			return store.OperatorRequeueTask(ctx, task.PlanID, task.TaskID, plandag.TaskEventPayload{
				Reason:         task.LatestPayload.Reason,
				OperatorAction: "requeue",
			}, task.VariantHint, false)
		}, fmt.Sprintf("requeued %s/%s", task.PlanSlug, task.TaskID))}
	}
}

func retryTaskCmd(repo string, task taskSummary) tea.Cmd {
	return func() tea.Msg {
		return serviceMsg{text: performPlanMutation(repo, func(ctx context.Context, store *plandag.Store) error {
			return store.OperatorRetryTask(ctx, task.PlanID, task.TaskID, plandag.TaskEventPayload{
				Reason:         task.LatestPayload.Reason,
				OperatorAction: "retry",
				Retryable:      true,
			})
		}, fmt.Sprintf("retry requested for %s/%s", task.PlanSlug, task.TaskID))}
	}
}

func failTaskCmd(repo string, task taskSummary) tea.Cmd {
	return func() tea.Msg {
		return serviceMsg{text: performPlanMutation(repo, func(ctx context.Context, store *plandag.Store) error {
			return store.OperatorFailTask(ctx, task.PlanID, task.TaskID, plandag.TaskEventPayload{
				Reason:         firstNonEmpty(task.LatestPayload.Reason, "operator requested failure"),
				OperatorAction: "fail",
			})
		}, fmt.Sprintf("force-failed %s/%s", task.PlanSlug, task.TaskID))}
	}
}

func handoffTaskCmd(repo string, task taskSummary, input string) tea.Cmd {
	return func() tea.Msg {
		variantName, reason := parseHandoffInput(input)
		if variantName == "" {
			return serviceMsg{text: "handoff failed: enter variant[:reason]"}
		}
		return serviceMsg{text: performPlanMutation(repo, func(ctx context.Context, store *plandag.Store) error {
			return store.OperatorHandoffTask(ctx, task.PlanID, task.TaskID, plandag.TaskEventPayload{
				Reason:         reason,
				HandoffTo:      variantName,
				OperatorAction: "handoff",
			}, variantName, false)
		}, fmt.Sprintf("handed off %s/%s to %s", task.PlanSlug, task.TaskID, variantName))}
	}
}

func markDoneTaskCmd(repo string, task taskSummary) tea.Cmd {
	return func() tea.Msg {
		return serviceMsg{text: performPlanMutation(repo, func(ctx context.Context, store *plandag.Store) error {
			sessionID := task.ClaimedBySession
			if sessionID == "" {
				sessID, err := store.CreateSession(ctx, plandag.SessionOpts{
					Mode:         plandag.SessionModeAttached,
					Transport:    plandag.SessionTransportStdio,
					PID:          os.Getpid(),
					PIDStartTime: "operator",
					Host:         "local",
				})
				if err != nil {
					return err
				}
				svID, err := store.CreateSessionVariant(ctx, plandag.SessionVariantOpts{
					SessionID:           sessID,
					VariantName:         "operator",
					SubprocessPID:       os.Getpid(),
					SubprocessStartTime: "operator",
				})
				if err != nil {
					return err
				}
				if _, err := store.ClaimNextReady(ctx, task.PlanID, "operator", sessID, svID); err != nil {
					return err
				}
				sessionID = sessID
			}
			evidence, _ := json.Marshal(plandag.TaskEventPayload{
				Summary:        "operator marked task done from TUI",
				OperatorAction: "mark_done",
			})
			_, err := store.MarkDone(ctx, task.PlanID, task.TaskID, sessionID, string(evidence))
			return err
		}, fmt.Sprintf("marked done %s/%s", task.PlanSlug, task.TaskID))}
	}
}

func performPlanMutation(repo string, fn func(context.Context, *plandag.Store) error, success string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := openPlanStore(ctx)
	if err != nil {
		return "mutation failed: " + err.Error()
	}
	defer func() { _ = store.Close() }()
	if _, err := os.Stat(repo); err != nil {
		return "mutation failed: " + err.Error()
	}
	if err := fn(ctx, store); err != nil {
		return "mutation failed: " + err.Error()
	}
	return success
}

func parseHandoffInput(input string) (variantName, reason string) {
	left, right, found := strings.Cut(strings.TrimSpace(input), ":")
	variantName = strings.TrimSpace(left)
	if found {
		reason = strings.TrimSpace(right)
	}
	return variantName, reason
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
