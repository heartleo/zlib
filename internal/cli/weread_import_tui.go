package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	keys "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/heartleo/zlib/internal/weread"
)

type wereadProgressMsg struct{ progress weread.ImportProgress }
type wereadImportedMsg struct{ result weread.ImportResult }

// wereadImportSteps names the import stages in order, indexed by
// weread.ImportStage.
var wereadImportSteps = [...]string{"Prepare upload", "Upload file", "Add to shelf"}

// wereadImportModel uploads one file and shows each stage of the import.
// Esc cancels and waits for WeRead to be told, so the result page can say
// whether the book may still appear.
type wereadImportModel struct {
	width, height int
	path          string
	account       string
	client        *weread.Client
	spinner       spinner.Model
	progress      progress.Model
	stage         weread.ImportStage
	sent, total   int64
	attempt       int
	attempts      int
	cancelling    bool
	result        weread.ImportResult
	err           error
	done          bool
	ch            chan tea.Msg
	ctx           context.Context
	cancel        context.CancelFunc
	// started is shared by every copy of the model, so the import runs once
	// however many times Init runs. A second upload would spend another
	// import.
	started *atomic.Bool
}

func newWeReadImportModel(path string, c *weread.Client) wereadImportModel {
	ctx, cancel := context.WithCancel(context.Background())
	var total int64
	if info, err := os.Stat(path); err == nil {
		total = info.Size()
	}
	var account string
	if c != nil {
		account = wereadAccountLabel(c.Credentials())
	}
	bar := progress.New(progress.WithDefaultBlend(), progress.WithWidth(40))
	return wereadImportModel{
		path:     path,
		account:  account,
		client:   c,
		spinner:  newLoadingSpinner(),
		progress: bar,
		total:    total,
		width:    80,
		height:   24,
		ch:       make(chan tea.Msg, 64),
		ctx:      ctx,
		cancel:   cancel,
		started:  new(atomic.Bool),
	}
}

// Init starts the import; its events arrive through m.ch. The program
// initializes the current page when it starts, so Init can run more than once;
// only the first call starts the import.
func (m wereadImportModel) Init() tea.Cmd {
	ch, ctx, c, path := m.ch, m.ctx, m.client, m.path
	if m.started.CompareAndSwap(false, true) {
		go func() {
			defer close(ch)
			result, err := c.ImportBook(ctx, path, func(p weread.ImportProgress) {
				// Drop updates the page has not caught up with; the next one
				// carries the whole state.
				select {
				case ch <- wereadProgressMsg{p}:
				default:
				}
			})
			if err != nil {
				sendLast(ch, errMsg{err})
				return
			}
			sendLast(ch, wereadImportedMsg{result})
		}()
	}
	return tea.Batch(m.spinner.Tick, waitForMsg(ch))
}

// sendLast delivers a final message without blocking, discarding a pending
// progress update when the buffer is full. The page reads until the
// channel closes, so the outcome is never lost.
func sendLast(ch chan tea.Msg, msg tea.Msg) {
	for {
		select {
		case ch <- msg:
			return
		default:
			select {
			case <-ch:
			default:
			}
		}
	}
}

func (m wereadImportModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.progress.SetWidth(max(10, min(48, msg.Width-30)))
	case wereadProgressMsg:
		p := msg.progress
		m.stage, m.total, m.attempt, m.attempts = p.Stage, p.Total, p.Attempt, p.Attempts
		m.sent = p.Sent
		var cmd tea.Cmd
		if p.Stage == weread.StageUpload && p.Total > 0 {
			cmd = m.progress.SetPercent(float64(p.Sent) / float64(p.Total))
		}
		return m, tea.Batch(cmd, waitForMsg(m.ch))
	case wereadImportedMsg:
		m.result, m.done = msg.result, true
		return m, tea.Quit
	case errMsg:
		m.err = msg.err
		switch {
		case m.err == nil:
			m.err = errors.New("WeRead import stopped unexpectedly")
		case m.cancelling && errors.Is(m.err, context.Canceled):
			m.err = errCancelled
		}
		return m, tea.Quit
	case tea.KeyPressMsg:
		if s := msg.String(); s == "esc" || s == "ctrl+c" {
			if !m.cancelling {
				m.cancelling = true
				m.cancel()
				return m, nil
			}
			if s == "ctrl+c" {
				// Stop waiting for WeRead to confirm the cancellation.
				m.err = errCancelled
				if m.stage == weread.StageProcess {
					m.err = fmt.Errorf("stopped waiting: %w", weread.ErrImportUnconfirmed)
				}
				return m, tea.Quit
			}
		}
	case progress.FrameMsg:
		var cmd tea.Cmd
		m.progress, cmd = m.progress.Update(msg)
		return m, cmd
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

// stepLines renders the stage list with the current stage's detail.
func (m wereadImportModel) stepLines() []string {
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	done := lipgloss.NewStyle().Foreground(currentTheme.Success)
	current := lipgloss.NewStyle().Foreground(currentTheme.Title).Bold(true)
	var lines []string
	for i, name := range wereadImportSteps {
		stage := weread.ImportStage(i)
		switch {
		case stage < m.stage:
			lines = append(lines, "  "+done.Render(symbolSuccess)+" "+name)
		case stage > m.stage:
			lines = append(lines, "  "+muted.Render("○ "+name))
		default:
			// Dot frames carry their own trailing space.
			lines = append(lines, "  "+m.spinner.View()+current.Render(name))
			if detail := m.stageDetail(); detail != "" {
				lines = append(lines, "    "+detail)
			}
		}
	}
	return lines
}

func (m wereadImportModel) stageDetail() string {
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	switch m.stage {
	case weread.StagePrepare:
		return muted.Render("Asking WeRead for an upload slot")
	case weread.StageUpload:
		return m.progress.View() + muted.Render(fmt.Sprintf("  %s / %s", formatBytes(m.sent), formatBytes(m.total)))
	case weread.StageProcess:
		text := "WeRead is reading the book"
		if m.attempt > 1 {
			text += fmt.Sprintf(" · check %d of %d", m.attempt, m.attempts)
		}
		return muted.Render(text)
	}
	return ""
}

func (m wereadImportModel) View() tea.View {
	subtitle := bookCell(filepath.Base(m.path))
	if m.total > 0 {
		subtitle += " · " + formatBytes(m.total)
	}
	var status string
	if m.account != "" {
		status = "To " + m.account
	}
	bindings := []keys.Binding{uiKey("esc", "cancel")}
	if m.cancelling {
		status = "Cancelling…"
		if m.stage == weread.StageProcess {
			status = "Cancelling… asking WeRead to drop the upload"
		}
		bindings = []keys.Binding{uiKey("ctrl+c", "stop waiting")}
	}
	return screenView(m.width, m.height, "Send to WeRead", subtitle, strings.Join(m.stepLines(), "\n"), status, bindings...)
}
