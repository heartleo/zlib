package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	keys "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/heartleo/zlib/internal/weread"
	"rsc.io/qr"
)

// — WeRead login page —

type wereadQRMsg struct{ code *qr.Code }
type wereadScannedMsg struct{}
type wereadLoggedInMsg struct{ creds weread.Credentials }

// wereadLoginModel shows the WeChat QR code and waits for the user to
// confirm the login on their phone. An expired or declined code can be
// replaced with r without leaving the page.
type wereadLoginModel struct {
	width, height int
	client        *weread.Client
	spinner       spinner.Model
	code          *qr.Code
	scanned       bool
	// retry holds the reason a fresh code is needed (expired or declined).
	retry  string
	creds  weread.Credentials
	err    error
	ch     chan tea.Msg
	ctx    context.Context
	cancel context.CancelFunc
	// started is shared by every copy of the model, so the flow starts once
	// however many times Init runs.
	started *atomic.Bool
}

func newWeReadLoginModel(c *weread.Client) wereadLoginModel {
	ctx, cancel := context.WithCancel(context.Background())
	return wereadLoginModel{client: c, spinner: newLoadingSpinner(), width: 80, height: 24,
		ch: make(chan tea.Msg, 4), ctx: ctx, cancel: cancel, started: new(atomic.Bool)}
}

// finished reports whether the page is done: logged in, failed or cancelled.
func (m wereadLoginModel) finished() bool {
	return m.err != nil || m.creds.Valid()
}

// Init starts the login flow; its events arrive through m.ch. The program
// initializes the current page when it starts, after a standalone command has
// already opened it, so Init can run more than once. Only the first call starts
// the flow: runWeReadLogin closes m.ch, and two of them on one channel would
// close it twice.
func (m wereadLoginModel) Init() tea.Cmd {
	if m.started.CompareAndSwap(false, true) {
		go runWeReadLogin(m.ctx, m.client, m.ch)
	}
	return tea.Batch(m.spinner.Tick, waitForMsg(m.ch))
}

// runWeReadLogin requests a QR code, waits for its confirmation and saves
// the login, reporting each step on ch. It closes ch when done.
func runWeReadLogin(ctx context.Context, c *weread.Client, ch chan<- tea.Msg) {
	defer close(ch)
	emit := func(msg tea.Msg) {
		select {
		case ch <- msg:
		case <-ctx.Done():
		}
	}
	qrCode, err := c.RequestQRCode(ctx)
	if err == nil {
		var code *qr.Code
		if code, err = encodeQR(qrCode.URL); err == nil {
			emit(wereadQRMsg{code})
			var creds weread.Credentials
			creds, err = finishWeReadLogin(ctx, c, qrCode, func() { emit(wereadScannedMsg{}) })
			if err == nil {
				emit(wereadLoggedInMsg{creds})
				return
			}
		}
	}
	emit(errMsg{err})
}

// restart requests a new QR code on a fresh channel.
func (m wereadLoginModel) restart() (wereadLoginModel, tea.Cmd) {
	m.cancel()
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.ch = make(chan tea.Msg, 4)
	m.code, m.scanned, m.retry = nil, false, ""
	go runWeReadLogin(m.ctx, m.client, m.ch)
	return m, tea.Batch(m.spinner.Tick, waitForMsg(m.ch))
}

func (m wereadLoginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case wereadQRMsg:
		m.code = msg.code
		return m, waitForMsg(m.ch)
	case wereadScannedMsg:
		m.scanned = true
		return m, waitForMsg(m.ch)
	case wereadLoggedInMsg:
		m.creds = msg.creds
		return m, tea.Quit
	case errMsg:
		switch {
		case errors.Is(msg.err, weread.ErrQRExpired):
			m.retry = "The QR code expired."
			return m, nil
		case errors.Is(msg.err, weread.ErrQRDeclined):
			m.retry = "The login was declined in WeChat."
			return m, nil
		}
		m.err = msg.err
		if m.err == nil {
			m.err = errors.New("WeRead login stopped unexpectedly")
		}
		return m, tea.Quit
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c", "q":
			m.cancel()
			m.err = errCancelled
			return m, tea.Quit
		case "r":
			if m.retry != "" {
				return m.restart()
			}
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

// loginSteps lists what the user does, marking finished steps, the current
// step and the ones to come, as the import page does.
func (m wereadLoginModel) loginSteps() []string {
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	done := lipgloss.NewStyle().Foreground(currentTheme.Success)
	current := lipgloss.NewStyle().Foreground(currentTheme.Title).Bold(true)
	step := func(state int, text string) string {
		switch state {
		case 0:
			return done.Render(symbolSuccess) + " " + text
		case 1:
			return m.spinner.View() + current.Render(text)
		}
		return muted.Render("○ " + text)
	}
	first, second := 1, 2
	if m.scanned {
		first, second = 0, 1
	}
	return []string{
		lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true).Render("Log in with WeChat"),
		"",
		step(first, "Open WeChat and scan this code"),
		step(second, "Confirm the login on your phone"),
		"",
		muted.Render("The code expires in 5 minutes."),
	}
}

func (m wereadLoginModel) View() tea.View {
	const title, subtitle = "WeRead login", "Signs in as the WeRead e-ink app"
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	bindings := []keys.Binding{uiKey("esc", "cancel")}
	switch {
	case m.retry != "":
		bindings = append([]keys.Binding{uiKey("r", "new code")}, bindings...)
		return tightScreenView(m.width, m.height, title, subtitle,
			[]string{"  " + m.retry, "  Press r for a new code."}, "", bindings...)
	case m.code == nil:
		return tightScreenView(m.width, m.height, title, subtitle,
			[]string{"  " + m.spinner.View() + "Requesting a QR code…"}, "", bindings...)
	}
	quiet, ok := qrFit(m.code.Size, m.width, m.height)
	if !ok {
		needW, needH := qrSizeNeeded(m.code.Size)
		hint := fitText(m.width-2,
			fmt.Sprintf("Enlarge the terminal to at least %d×%d to show the QR code.", needW, needH),
			fmt.Sprintf("Enlarge the terminal: %d×%d", needW, needH),
			fmt.Sprintf("Need %d×%d", needW, needH))
		return tightScreenView(m.width, m.height, title, subtitle, []string{"  " + hint}, "", bindings...)
	}
	body, beside := layoutQR(strings.Split(renderQRCode(m.code, quiet), "\n"), m.loginSteps(), m.width)
	// Center the code in the rows the page leaves it.
	body = append(make([]string, max(0, (m.height-qrReserveRows-len(body))/2)), body...)
	var status string
	switch {
	case m.scanned:
		status = lipgloss.NewStyle().Foreground(currentTheme.Success).Render(fitText(m.width,
			symbolSuccess+" Scanned. Confirm the login in WeChat on your phone.",
			symbolSuccess+" Scanned. Confirm on your phone."))
	case beside:
		status = muted.Render("Waiting for the scan…")
	default:
		status = muted.Render(fitText(m.width,
			"Scan with WeChat, then confirm on your phone. The code expires in 5 minutes.",
			"Scan with WeChat, then confirm on your phone.",
			"Scan with WeChat."))
	}
	return tightScreenView(m.width, m.height, title, subtitle, body, status, bindings...)
}
