package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	keys "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/weread"
	"github.com/heartleo/zlib/internal/zlib"
	"github.com/spf13/cobra"
)

var downloadCmd = &cobra.Command{
	Use:   "download <book-id>",
	Short: "Download a book by ID",
	Long:  "Download a book by its id:hash ID from search, favorites, or history results.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := destDirFlag(cmd)
		if err != nil {
			return err
		}
		c, err := loadClient()
		if err != nil {
			return err
		}
		return runDownload(newDownloadModel(args[0], dir, c), deliveryFlags(cmd), cmd)
	},
}

// deliveries selects where a finished download is sent.
type deliveries struct {
	kindle, weread bool
}

// addDeliveryFlags registers --send-to-kindle and --send-to-weread.
func addDeliveryFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("send-to-kindle", false, "Send the downloaded book to Kindle")
	cmd.Flags().Bool("send-to-weread", false, "Import the downloaded book into WeRead")
}

func deliveryFlags(cmd *cobra.Command) deliveries {
	kindle, _ := cmd.Flags().GetBool("send-to-kindle")
	weread, _ := cmd.Flags().GetBool("send-to-weread")
	return deliveries{kindle: kindle, weread: weread}
}

// downloadResult is the --json document a finished download prints.
type downloadResult struct {
	ID     string        `json:"id,omitempty"`
	Name   string        `json:"name,omitempty"`
	Path   string        `json:"path"`
	Size   int64         `json:"size"`
	Kindle *kindleResult `json:"kindle,omitempty"`
	WeRead *wereadResult `json:"weread,omitempty"`
}

// kindleResult reports the Send to Kindle step of a download.
type kindleResult struct {
	Sent  bool   `json:"sent"`
	To    string `json:"to,omitempty"`
	Error string `json:"error,omitempty"`
}

// runDownload executes the download described by m — through the bubbletea
// progress UI on an interactive terminal, or a plain path otherwise (see
// isInteractive) — then reports the saved file and optionally delivers it to
// Kindle and WeRead. Shared by the download, search, history, favorites and
// feed commands. With --json the plain path always runs, progress goes to
// stderr, and stdout carries only the downloadResult document.
func runDownload(m downloadModel, deliver deliveries, cmd *cobra.Command) error {
	if m.autoKindle {
		deliver.kindle = true
	}
	jsonOut := false
	if cmd.Flags().Lookup("json") != nil {
		jsonOut, _ = cmd.Flags().GetBool("json")
	}
	if !jsonOut && !m.autoKindle {
		dir, proceed, err := chooseDownloadDirectory(cmd, m.dir)
		if err != nil {
			return err
		}
		if !proceed {
			return errCancelled
		}
		m.dir = dir
	}
	var dm downloadModel
	var err error
	if jsonOut || !isInteractive() {
		dm, err = m.runPlain(os.Stderr)
	} else {
		dm, err = runDownloadProgram(m)
	}
	if err != nil {
		return err
	}
	if dm.state == stateCancelled {
		return errCancelled
	}
	if dm.err != nil {
		return dm.err
	}

	path := dm.savedPath
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	result := downloadResult{ID: dm.id, Name: dm.bookName, Path: path, Size: dm.savedSize}
	if !jsonOut {
		lipgloss.Fprintln(os.Stdout)
		lipgloss.Fprintf(os.Stdout, "%s Saved to: %s (%d bytes)\n",
			colorGreen(symbolSuccess),
			colorGreen(path),
			dm.savedSize,
		)
	}
	// Each target runs even when the other failed; the first failure
	// decides the exit status.
	var deliveryErr error
	if deliver.kindle {
		to, err := sendDownloadedFileToKindle(path, !jsonOut)
		result.Kindle = &kindleResult{Sent: err == nil, To: to}
		if err != nil {
			result.Kindle.Error = err.Error()
			deliveryErr = &deliveryError{target: targetKindle, path: path, err: err}
		}
	}
	if deliver.weread {
		imported, err := sendDownloadedFileToWeRead(path, !jsonOut)
		result.WeRead = newWeReadResult(imported, err)
		if err != nil {
			if hint := wereadErrorHint(err, wereadLoginCLI); hint != "" {
				result.WeRead.Error += ". " + hint
			}
			if deliveryErr == nil {
				deliveryErr = &deliveryError{target: targetWeRead, path: path, err: err}
			} else if !jsonOut {
				lipgloss.Fprintln(os.Stderr, formatCLIError(&deliveryError{target: targetWeRead, path: path, err: err}))
			}
		}
	}
	if jsonOut {
		if err := printJSON(result); err != nil {
			return err
		}
	}
	return deliveryErr
}

// runDownloadProgram runs the download through the bubbletea progress UI.
func runDownloadProgram(m downloadModel) (downloadModel, error) {
	result, err := tea.NewProgram(m).Run()
	if err != nil {
		return downloadModel{}, err
	}
	return result.(downloadModel), nil
}

// runPlain performs the download without the bubbletea UI, for non-terminal
// output and --json. It mirrors the fetch-then-download flow of the UI model
// and records the result on the returned model. Status and progress lines go
// to status (stderr), keeping stdout for the result. Ctrl+C cancels the
// transfer, so the library can clean up its partial file before the command
// exits.
func (m downloadModel) runPlain(status io.Writer) (downloadModel, error) {
	downloadURL, name := m.directURL, m.bookName
	if downloadURL == "" {
		book, err := m.client.FetchBook(m.id)
		if err != nil {
			return m, fmt.Errorf("failed to fetch book: %w", err)
		}
		if book.DownloadURL == "" {
			return m, fmt.Errorf("no download URL available for book %s", m.id)
		}
		downloadURL, name = book.DownloadURL, book.Name
	}
	m.bookName = name

	// Trap Ctrl+C only for the transfer; during the fetch it still exits
	// immediately, since nothing has been written yet.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// The title comes from the server; bookCell strips control sequences.
	fmt.Fprintf(status, "Downloading: %s\n", bookCell(name))
	progress := plainProgress{w: status}
	result, err := m.client.DownloadWithContext(ctx, downloadURL, m.dir, progress.report)
	if err != nil {
		if ctx.Err() != nil {
			m.state = stateCancelled
			return m, nil
		}
		return m, err
	}
	m.savedPath = result.FilePath
	m.savedSize = result.Size
	m.state = stateComplete
	return m, nil
}

// plainProgress prints download progress as plain lines a log or an agent can
// follow: one line per 10% when the size is known, otherwise one per
// plainProgressChunk bytes.
type plainProgress struct {
	w        io.Writer
	reported int64 // last percent step, or bytes when the total is unknown
}

const plainProgressChunk = 5 << 20

func (p *plainProgress) report(written, total int64) {
	const mb = 1 << 20
	if total > 0 {
		step := written * 100 / total / 10 * 10
		if step > p.reported {
			p.reported = step
			fmt.Fprintf(p.w, "  %d%% (%.1f MB / %.1f MB)\n", step, float64(written)/mb, float64(total)/mb)
		}
		return
	}
	if written-p.reported >= plainProgressChunk {
		p.reported = written
		fmt.Fprintf(p.w, "  %.1f MB\n", float64(written)/mb)
	}
}

func init() {
	addDestDirFlag(downloadCmd)
	addDeliveryFlags(downloadCmd)
	downloadCmd.Flags().Bool("json", false, "Print the saved file as JSON; progress goes to stderr")
}

// — bubbletea model —

type downloadState int

const (
	stateFetching downloadState = iota
	stateDownloading
	stateComplete
	stateCancelled
)

type downloadModel struct {
	width, height  int
	id             string
	dir            string
	client         *zlib.Client
	state          downloadState
	bookName       string
	directURL      string // set to skip fetch and download immediately
	spinner        spinner.Model
	progress       progress.Model
	written        int64
	total          int64
	started        time.Time
	savedPath      string
	savedSize      int64
	stayOnComplete bool
	completeOffset int
	completeAction int
	sendToKindle   bool
	autoKindle     bool // explicit list shortcut; skip directory and send dialogs
	sendToWeRead   bool
	// completeMessage reports a delivery outcome on the completion page.
	completeMessage string
	completeTone    noticeTone
	err             error
	downloadCh      chan tea.Msg
	cancel          context.CancelFunc
}

const downloadProgressInterval = 66 * time.Millisecond

type fetchDoneMsg struct{ book zlib.Book }
type progressMsg struct{ written, total int64 }
type downloadDoneMsg struct {
	path string
	size int64
}
type errMsg struct{ err error }

func newDownloadModel(id, dir string, c *zlib.Client) downloadModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(currentTheme.Accent)

	// percentLabel shows the real fraction; the bar's own percentage would
	// follow its animation and lag behind it.
	p := progress.New(
		progress.WithDefaultBlend(),
		progress.WithWidth(40),
		progress.WithoutPercentage(),
	)

	return downloadModel{
		id:       id,
		dir:      dir,
		client:   c,
		spinner:  s,
		progress: p,
	}
}

// newDirectDownloadModel skips the fetch phase and downloads from a known URL.
func newDirectDownloadModel(bookName, downloadURL, dir string, c *zlib.Client) downloadModel {
	m := newDownloadModel("", dir, c)
	m.bookName = bookName
	m.directURL = downloadURL
	return m
}

func (m downloadModel) Init() tea.Cmd {
	if m.state == stateComplete {
		return nil
	}
	if m.directURL != "" {
		// Emit a synthetic fetchDoneMsg to skip the HTTP fetch.
		book := zlib.Book{DownloadURL: m.directURL, Name: m.bookName}
		return func() tea.Msg { return fetchDoneMsg{book} }
	}
	return tea.Batch(m.spinner.Tick, m.fetchCmd())
}

func (m downloadModel) fetchCmd() tea.Cmd {
	return func() tea.Msg {
		book, err := m.client.FetchBook(m.id)
		if err != nil {
			return errMsg{fmt.Errorf("failed to fetch book: %w", err)}
		}
		if book.DownloadURL == "" {
			return errMsg{fmt.Errorf("no download URL available for book %s", m.id)}
		}
		return fetchDoneMsg{book}
	}
}

// waitForMsg returns a Cmd that reads the next message from the download channel.
func waitForMsg(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func (m downloadModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.progress.SetWidth(max(1, min(60, msg.Width-8)))
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, tea.Quit

	case fetchDoneMsg:
		m.state = stateDownloading
		m.bookName = msg.book.Name
		ch := make(chan tea.Msg, 64)
		m.downloadCh = ch
		m.started = time.Now()
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		go func() {
			emit := func(msg tea.Msg) {
				select {
				case ch <- msg:
				case <-ctx.Done():
				}
			}
			var last time.Time
			result, err := m.client.DownloadWithContext(ctx, msg.book.DownloadURL, m.dir, func(w, t int64) {
				// Reads arrive per buffer; redraw at most ~15 times a second.
				if now := time.Now(); now.Sub(last) >= downloadProgressInterval || (t > 0 && w >= t) {
					last = now
					emit(progressMsg{w, t})
				}
			})
			if err != nil {
				emit(errMsg{err})
			} else {
				emit(downloadDoneMsg{result.FilePath, result.Size})
			}
			close(ch)
		}()
		if m.directURL != "" {
			// No fetch phase ran, so no spinner tick chain exists yet.
			return m, tea.Batch(waitForMsg(ch), m.spinner.Tick)
		}
		return m, waitForMsg(ch)

	case tea.KeyPressMsg:
		if m.state == stateComplete && m.stayOnComplete {
			actions := m.completeActions()
			m.completeAction = min(max(0, m.completeAction), len(actions)-1)
			switch msg.String() {
			case "enter":
				m.sendToKindle = actions[m.completeAction] == actionSendToKindle
				m.sendToWeRead = actions[m.completeAction] == actionSendToWeRead
				return m, tea.Quit
			case "esc", "q", "ctrl+c":
				m.sendToKindle, m.sendToWeRead = false, false
				return m, tea.Quit
			case "left", "shift+tab":
				m.completeAction = (m.completeAction + len(actions) - 1) % len(actions)
			case "right", "tab":
				m.completeAction = (m.completeAction + 1) % len(actions)
			case "up", "k":
				m.completeOffset--
			case "down", "j":
				m.completeOffset++
			case "pgup":
				m.completeOffset -= max(1, m.height-6)
			case "pgdown":
				m.completeOffset += max(1, m.height-6)
			}
			m.completeOffset = min(max(0, m.completeOffset), m.maxCompleteOffset())
			return m, nil
		}
		if msg.String() == "ctrl+c" || msg.String() == "esc" {
			if m.cancel != nil {
				m.cancel()
			}
			m.state = stateCancelled
			return m, tea.Quit
		}

	case progressMsg:
		m.written = msg.written
		m.total = msg.total
		var pct float64
		if m.total > 0 {
			pct = float64(m.written) / float64(m.total)
		}
		progressCmd := m.progress.SetPercent(pct)
		return m, tea.Batch(progressCmd, waitForMsg(m.downloadCh))

	case downloadDoneMsg:
		m.savedPath = msg.path
		if path, err := filepath.Abs(msg.path); err == nil {
			m.savedPath = path
		}
		m.savedSize = msg.size
		m.state = stateComplete
		if m.autoKindle {
			m.sendToKindle = true
			return m, tea.Quit
		}
		if m.stayOnComplete {
			return m, nil
		}
		return m, tea.Quit

	case progress.FrameMsg:
		pm, cmd := m.progress.Update(msg)
		m.progress = pm
		return m, cmd

	case spinner.TickMsg:
		if m.state == stateFetching || (m.state == stateDownloading && m.written == 0) {
			sm, cmd := m.spinner.Update(msg)
			m.spinner = sm
			return m, cmd
		}
	}
	return m, nil
}

func (m downloadModel) View() tea.View {
	if m.state == stateComplete && m.stayOnComplete {
		return m.completeView()
	}
	return screenView(m.width, m.height, "Download", bookCell(m.bookName), m.viewContent(), "Directory: "+bookCell(m.dir), uiKey("esc", "cancel"))
}

func (m downloadModel) completeLines() []string {
	path := strings.NewReplacer("\r", " ", "\n", " ").Replace(ansi.Strip(m.savedPath))
	return append([]string{"Saved to:"}, strings.Split(ansi.Hardwrap(path, max(1, m.width-2), false), "\n")...)
}

func (m downloadModel) maxCompleteOffset() int {
	return max(0, len(m.completeLines())-m.completePathRows())
}

// Labels of the download-complete actions.
const (
	actionBack         = "Back"
	actionSendToKindle = "Send to Kindle"
	actionSendToWeRead = "Send to WeRead"
)

func (m downloadModel) canSendToKindle() bool {
	return slices.Contains(kindleAllowedTypes, strings.ToLower(filepath.Ext(m.savedPath)))
}

// completeActions lists Back plus each delivery that accepts the file.
func (m downloadModel) completeActions() []string {
	actions := []string{actionBack}
	if m.canSendToKindle() {
		actions = append(actions, actionSendToKindle)
	}
	if weread.SupportedFormat(m.savedPath) {
		actions = append(actions, actionSendToWeRead)
	}
	return actions
}

func (m downloadModel) completePathRows() int {
	return max(1, m.height-6-len(actionButtonRows(m.completeActions(), m.completeAction+1, m.width))-1)
}

func (m downloadModel) completeView() tea.View {
	lines := m.completeLines()
	buttons := actionButtonRows(m.completeActions(), m.completeAction+1, m.width)
	visible := m.completePathRows()
	start := min(max(0, m.completeOffset), max(0, len(lines)-visible))
	status := m.completeMessage
	switch {
	case status != "" && m.completeTone == noticeSuccess:
		status = colorGreen(symbolSuccess + " " + status)
	case status != "" && m.completeTone == noticeError:
		status = colorRed(symbolError + " " + status)
	}
	if status == "" && len(lines) > visible {
		status = fmt.Sprintf("Path lines %d–%d of %d", start+1, min(start+visible, len(lines)), len(lines))
	}
	bindings := []keys.Binding{uiKey("enter", "back")}
	if len(m.completeActions()) > 1 {
		bindings = []keys.Binding{uiKey("←/→", "select"), uiKey("enter", "open")}
	}
	if len(lines) > visible {
		bindings = append(bindings, uiKey("↑/↓", "scroll"))
	}
	body := append([]string{}, lines[start:min(len(lines), start+visible)]...)
	body = append(body, "")
	body = append(body, buttons...)
	return screenView(m.width, m.height, "Download complete", bookCell(m.bookName), strings.Join(body, "\n"), status,
		bindings...)
}

func (m downloadModel) percentLabel() string {
	if m.total <= 0 {
		return ""
	}
	return fmt.Sprintf("%3d%%", min(100, int(m.written*100/m.total)))
}

// transferDetail reads "12.3 MiB / 29.0 MiB · 3.1 MiB/s · 5s left"; the rate
// and ETA appear once enough time has passed for a stable figure.
func (m downloadModel) transferDetail() string {
	parts := []string{formatBytes(m.written)}
	if m.total > 0 {
		parts[0] += " / " + formatBytes(m.total)
	}
	if m.state != stateDownloading || m.started.IsZero() {
		return parts[0]
	}
	elapsed := time.Since(m.started)
	if rate := transferRate(m.written, elapsed); rate != "" {
		parts = append(parts, rate)
		if m.total > m.written {
			remaining := time.Duration(float64(m.total-m.written) / (float64(m.written) / elapsed.Seconds()) * float64(time.Second))
			parts = append(parts, remaining.Round(time.Second).String()+" left")
		}
	}
	return strings.Join(parts, " · ")
}

func (m downloadModel) viewContent() string {
	switch m.state {
	case stateFetching:
		return fmt.Sprintf("  %s Looking up book %s…\n",
			m.spinner.View(), colorCyan(m.id))
	case stateDownloading, stateComplete, stateCancelled:
		if m.state == stateDownloading && m.written == 0 {
			return fmt.Sprintf("  %s Connecting to the download server…\n", m.spinner.View())
		}
		bar := m.progress.View()
		if m.state == stateComplete {
			bar = m.progress.ViewAs(1.0)
		}
		out := fmt.Sprintf("  Downloading: %s\n  %s %s\n  %s\n",
			m.bookName, bar, m.percentLabel(), colorFaint(m.transferDetail()))
		if m.state == stateCancelled {
			out += fmt.Sprintf("\n  %s %s\n",
				colorRed(symbolError),
				colorRed("Download cancelled (incomplete file removed)"))
		}
		return out
	}
	return ""
}
