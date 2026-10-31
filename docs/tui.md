# Terminal interface

Run `zlib tui` in a terminal for a persistent shell. Press `/` on the home screen
to open commands, then choose Search to enter a book title. The home screen has
no search input; typing, pasting, and Ctrl+K do not open commands there.
Tab completes the selected command; Enter runs it, even
when you've typed only part of its name (for example, `/th` selects `/theme`).
`/quit` exits. The shell keeps the current book list and selection when you return from
a download, theme, settings or Kindle screen.

The signed-in start screen shows a five-row ZLIB wordmark painted with one
Accent-to-Link gradient across its columns, as Crush paints its logo. When a
theme's Link is too close to Accent for the ramp to show, Info or Success ends
the gradient (`wordmarkRampEnd`). The home screen shows no brand text, build
version, decorative side strokes or trademark sign; use `zlib version` for the
version. Below 32 columns or 12 rows, the word ZLIB in the same gradient
replaces it.
With at least 16 content rows, two blank rows separate the wordmark and command
hints; compact layouts use one. Command columns have a four-cell gutter; single-column hints fit their content.
It does not display the current directory or a persistent sign-in label. At 76
columns or wider, command hints use two columns; smaller terminals use one.
The hints show commands to type in the input; typing `/` opens the selectable
command menu. Without a saved session, `zlib tui` opens the email and password
form immediately. Press Esc to reach the signed-out start screen, where Enter
reopens login and `/` gives access to settings and domain checks. That screen
shows a heading, a focused Log in button rendered by `renderActionButton`, and
the sign-in method; it adds no other explanatory text. Command hints on the
signed-in screen use muted descriptions, matching the command dialog. A successful
login returns to the start screen. If you tried a protected command first, its
draft stays in the input so
you can press Enter to continue. Login errors stay on the form for another try.
The TUI login form accepts email and password. To import a Cookie file, use
`zlib login --cookies <file> --domain <domain>` outside the TUI.
`/logout` asks for confirmation, clears the saved session and account pages, then
opens the login form. If session removal fails, the current screen shows an error.

Typing `/` opens a centered command dialog over the current page. It keeps the
page and its selection in place. At 80×24, the dialog shows five commands,
each with its description below the name. Narrow terminals show one line per
command. ↑/↓ cycles through matches,
including from last to first; the list scrolls when selection leaves its visible
rows. Type to filter, Tab to complete into the command input, or Enter to run.
On book lists, Ctrl+K also opens the dialog. Esc closes it and restores the page.

Shell command feedback appears at the right end of the title row, so the list
status line below it stays visible. Errors start with ✗ in the error color and
stay until the next key press. Confirmations start with ✓ in the success color;
they and neutral hints disappear after five seconds. New feedback restarts the
timer; an older timer cannot clear a newer message. The input and bottom key
hints stay in place. Errors that need a response stay on their form or result
page.

Direct commands such as `zlib search` and `zlib history --json` still work.
The Search form shows query, year range, languages, formats, exact match, and sort
order together. With at least 20 rows the query sits in a rounded box, like a
chat composer, with an accent border while focused; shorter terminals use a
plain prompt line. The form is at most 100 columns wide. A Crush-style
`Filters ─── N active` section header precedes the filter rows, whose labels
share one 12-cell column; unset values (`Any`, `Default`, `[ ]`) are muted.
The status line above the key hints describes the focused field. A validation
error appears directly below its field, wraps instead of truncating, and clears
once that field is edited.
Enter opens a language, format, or sort picker; Space toggles a
language or format. Esc closes the picker. The borderless Search button uses the
accent background when focused. Tab moves between fields, Enter submits from
the button, and Ctrl+S submits from any field. Invalid years stay on the form.
Search loading shows the dot spinner with `Searching…` and the active filters.
The result status line appends the active filters after the page and position
bar while at least 12 cells remain; narrower terminals drop them. An empty
result lists the active filters and suggests loosening them. Request errors
wrap to the terminal width so the reason stays readable.
Direct `zlib search <query>` accepts `--from-year`, `--to-year`, repeatable
`--language` and `--ext`, `--exact`, and `--order`. An unset sort does not send
an `order` value. The result page reopens the same filters with Ctrl+F in the
shell or `/` in direct interactive search.
Bare `zlib` prints the home screen's ZLIB wordmark above command help, without
the version line. Use `zlib version` to see the full version.
`zlib tui` requires an interactive terminal.

The shell accepts `/search`, `/favorites`, `/recommended`, `/most-popular`,
`/history`, `/login`, `/logout`, `/profile`, `/doctor`,
`/config`, `/config network`, `/config download`, `/config kindle`, `/theme`, `/kindle`,
`/kindle config`, `/kindle send <file>` and `/quit`. Use `zlib version`
outside the TUI to check the installed version.
For automation, use direct commands and their flags, including `--json`.
To download a selected book in the TUI, press Enter on its list row. To download
by `id:hash` directly, use `zlib download "id:hash"` outside the TUI.
The search list keeps its scroll position while you move within visible rows.
If a search returns nothing or fails, reopen its filters with Ctrl+F, or retry a
failed request with r.
Search and the other book lists show a Charm dot spinner while fetching results.
Page numbers appear after loading. Profile, login, and settings domain checks
also use the dot spinner while waiting for a response.
Search results appear together when the request completes; rows do not fill in
over several frames. Search loading and empty states keep their controls at the
bottom of the page. An empty search shows its page and offers a query edit.
Doctor shows only the Charm dot spinner while checking domains; Esc returns.
`zlib doctor` opens the same page in an interactive terminal. Results contain
only available domains, without a status label on each row. The accent border
marks the focused row; `Current domain` marks the configured domain, which gets
initial focus when available. Moving the focus does not change configuration.
Redirected output uses a single-column table; `--json` keeps the probe fields.
The status row below the list title is the only position indicator: it reads
`Page 1 of 10 · Book 5 of 50` (feeds without pages show only `Book 5 of 50`).
The title row carries no counter and there is no progress bar; the EAPI reports
a page count but no total number of books, so lists never show one.
Search loads up to 50 books per server page, matching the direct `search` command.
The number of visible rows follows the terminal height; scroll within a page with
↑/↓ or PgUp/PgDn and change server pages with ←/→.

## Book list spacing

Search, Favorites, Recommended, Most popular, and History share one list layout:

- Put the title and status above the selected-book card. At 78 columns or wider
  with at least 20 page rows, the card uses 11 rows. From 30 to 77 columns it
  uses 13 rows when the page has at least 24 rows. Otherwise it uses 8 rows.
- The expanded card shows category, year, edition, publisher, language, pages,
  and ISBN 10/13 when available. Missing EAPI fields are left blank. Valid
  ISBN 10 and `978` ISBN 13 values can supply each other's equivalent.
- Leave one empty row between the card and the column header.
- At a page height of 15 rows or more, leave one empty row between the list area
  and the bottom controls.
- At a page height of 14 rows, omit the bottom gap to keep a book visible. Below
  14 rows or narrower than 30 columns, show the enlarge-terminal prompt.

Calculate visible book rows from the page height. The shell reserves space for
its footer and, while typing a command, its input. Keep `feedRows()` and
`feedViewWith()` in sync so scrolling, selection, and rendering agree. Use
`fullScreenView()` to fill any remaining terminal space. The cross-page layout
test is `TestBookListSpacingMatchesSearchAcrossPages`.

The selected book's EAPI details load after the selection settles. Results are
cached by `id:hash`; moving to another book keeps an older response from
replacing its card. The list response remains visible while details load.
The card border uses the theme accent, and its author uses the theme's primary
text color. Moving the selection updates the card and status position immediately,
without a color flash or animated position change.
History uses the same `#`, `Title`, and `Author` columns as the other book lists.
The selected entry's download date appears beside the page status. Its card uses
the book's author and metadata when EAPI provides them; moving within visible
history rows keeps the list viewport still.

## Controls

### Action buttons

Render every operation through `renderActionButton` in `tui_layout.go`. Forms,
confirmations, settings editors, download completion, and download-directory
choices use the same borderless button: two cells of horizontal padding, theme
surface and title color at rest, theme accent fill when focused. A focused
destructive action uses the theme error fill. The fill shows focus; do not add a
leading `>` or a separate button border. Paint gaps between buttons with the
panel background so ANSI resets do not expose the terminal background. Keep
Cancel focused when a confirmation opens. Use `actionButtonRows` to wrap groups
by terminal width. List entries and folder browser rows retain their list
selection style.

| Screen | Controls |
| --- | --- |
| Shell command input | `/` opens commands from a list; ↑/↓ cycles; Ctrl+K also opens it; Tab completes; Esc closes; pasted text needs Enter to run |
| Profile | Full-screen account and daily download quota; the used count sits above a width-aware horizontal bar. Wide terminals show account fields in two columns. ↑/↓ or PgUp/PgDn scrolls when needed; r retries a failed request; Esc returns. |
| Doctor | Full-screen available-domain list; the focused row has an accent border and the current configured domain has a separate marker; ↑/↓ selects, PgUp/PgDn scrolls, r checks again, Esc returns |
| Book lists | ↑/↓ or j/k select; PgUp/PgDn scroll; Home/End jump; Enter opens download confirmation; q or Esc exits |
| Search, saved books, history | ←/→ changes page |
| Search results | Ctrl+F reopens Search filters in the shell; `/` reopens them in direct `zlib search`. Other lists have no filters, so their footer does not offer Ctrl+F |
| Failed list request | r retries the failed page; Esc closes |
| Login, search options, Kindle settings | Enter edits a field; Enter applies an edit; Tab advances; choose the final action or press Ctrl+S to submit; Esc cancels |
| Format selection | Huh checkbox list: ↑/↓ moves; Space toggles; Ctrl+A selects all or clears all; Enter continues; no selected formats means all formats |
| Config | Network, download and Kindle settings appear in one compact list; arrows or Tab select a field; Enter opens it; Esc returns; Tab selects editor actions |
| Config domain | Your mirrors, then built-in domains, each with its check result and available rows first within each group; `●` marks the current domain; rows appear at once and fill in as checks finish. Enter uses an available row and explains why other rows cannot be used; `a` adds a mirror (check, then a trust dialog); `d` removes a mirror after confirmation; `r` checks again; Esc returns |
| File picker | Enter opens a folder or selects a file; ← goes to the parent; `/` accepts a directory path; Esc exits |
| Download directory | A centered dialog overlays the selected book list. Download here uses the displayed folder; Choose another folder opens the browser. Space selects a folder; e or / edits the path; Esc uses the displayed folder; Ctrl+C cancels. |
| Theme | Type to filter; ↑/↓ previews; Enter selects; Ctrl+N creates a custom theme; Ctrl+E edits a custom theme or copies a built-in; Ctrl+S saves color edits; Esc cancels or clears the filter |
| Confirmation | Cancel is selected initially; ←/→ or Tab changes the action; Enter confirms |
| Download | Esc or Ctrl+C cancels an active transfer. After completion, the page shows the full saved path and Back. Supported file types also show Send to Kindle; left/right selects an action and Enter opens it. Up/down scrolls a long path. Kindle sending asks for confirmation and returns to the saved-file page. A download failure stays on its own page with r to retry using the same book and directory, or Esc to return. |

Passwords are masked. Invalid field values stay in the editor with an error.
If the SMTP password is missing after Kindle confirmation, a masked form saves
it to the global `.env` and sets it for the current process before sending.
Cancel returns to the saved file; a save error keeps the form open.
Kindle SMTP sending cannot be recalled: Ctrl+C stops waiting, and the message
may still be delivered.
After a Kindle send error, the file and error remain visible. Check the Kindle
inbox before starting another send; the error page does not resend automatically.
Download errors hide signed URLs in the terminal view.

## Implementation references

Interactive `zlib profile` uses the same profile page as `/profile`, including
loading, retry, scrolling, and the quota bar. `--json` keeps its JSON output;
redirected text output uses the same profile fields without opening a TUI.
`zlib kindle`, `kindle config`, and `kindle send` reuse the corresponding shell
pages and transitions. Closing them returns to the terminal. Kindle send results
stay visible until dismissed; non-interactive sends do not open a confirmation.

Crush revision: `f49b28113b3bab3fcfe193676cc3f7fb858d6411`.
Compare against a local clone of
[charmbracelet/crush](https://github.com/charmbracelet/crush) (`<crush-checkout>`).
Check its current HEAD before comparing code with the revision above.
The implementation uses Bubbles v2, Huh v2 and Lip Gloss v2, with interaction patterns
from Crush's `internal/ui/dialog/models.go`, `filepicker.go`, `inline_editor.go`,
`dialog.go`, `internal/ui/list/list.go` and `internal/ui/model/status.go`.
Crush internal packages are not imported or copied.
See [Crush TUI patterns](crush-ui-patterns.md) for the component and layout analysis.

- `tui_layout.go`: terminal-sized screen layout, selected rows and Bubbles help.
- `tui_form.go`, `tui_inline_form.go`: field lists, masked text input, validation
  and Huh format checkboxes in the inline search form.
- `tui_filepicker.go`: Bubbles file picker, path input and send confirmation.
- `book_feed_view.go`: shared book list for search, recommendations, popular,
  favorites and history.
- `confirmation.go`: modal overlay with Cancel selected by default.

## Verification

Automated tests cover Unicode and paste, masked passwords, invalid inputs,
clearing format filters, cancellation, list retry, modal actions and dimensions
from 20×5 to 120×30. Small windows preserve controls or request more space.

The terminal walkthrough uses local fixtures and an HTTP server. It covers
recommendations, popular books, saved pagination, search refinement, history
confirmation, domain selection, download directory, Kindle file confirmation and
a 1 MiB download. It never contacts Z-Library or sends SMTP mail.

```powershell
go test -c ./internal/cli -o "$env:TEMP/zlib-tui-smoke.exe"
$env:ZLIB_TUI_SMOKE = '1'
& "$env:TEMP/zlib-tui-smoke.exe" '-test.run=^TestTUIWalkthrough$' '-test.v'
```

The walkthrough was run in a Windows terminal. Login masking and cancellation,
search query/format editing, theme preview/cancellation and isolated Kindle and
retry-setting saves were also exercised through `zlib.exe`.
