# Crush TUI patterns

Source: [charmbracelet/crush](https://github.com/charmbracelet/crush) at revision `f49b28113b3bab3fcfe193676cc3f7fb858d6411`.

## Structure

Crush has one top-level Bubble Tea model in `internal/ui/model/ui.go`. It owns terminal size, layout rectangles, focus, dialogs, and message routing. Most child components expose focused operations such as `Render`, `Draw`, `ScrollBy`, or `HandleMsg` instead of nesting full Bubble Tea models. `View` paints a screen buffer: rectangular regions for the shell, with string renderers for lists and completions. See `internal/ui/AGENTS.md` and `model/ui.go` (`Update`, `Draw`, `View`, `generateLayout`).

Expensive work runs in commands and returns messages. Components do not mutate the parent model from inside a command. This keeps loading, cancellation, and stale responses within the main update loop.

## Component behavior

| Component | Source | Pattern |
| --- | --- | --- |
| Lists | `internal/ui/list/list.go` | Selection and scroll offset are separate. Rendering stops at the visible row budget; items cache expensive rendering. |
| Command completion | `internal/ui/completions/completions.go` | Up/down wraps at the ends, keeps selection in view, and reports whether a key was consumed. |
| Dialogs | `internal/ui/dialog/dialog.go`, `common.go` | A dialog stack intercepts input before the page. Dialogs return actions; the parent handles the result. An optional input grace period protects dialogs opened while keys are still arriving. |
| Buttons | `internal/ui/common/button.go` | Text, focus, hover, inactive state, and padding are one rendering API. A separate hit map supports mouse clicks. Focus is visible through fill and text color; borders are not required. |
| Theme picker | `internal/ui/dialog/theme.go` | Navigation wraps and skips headers. Highlighting previews a palette; selecting commits it; closing reverts the preview. |
| Status | `internal/ui/model/status.go` | Key hints share a fixed region with short messages. Messages are truncated to the available cells. |

## Sizing and visual rules

Dialog width is measured after border and padding. `dialog/common.go` sizes the list to the inner content width and reserves a scrollbar column only when content overflows. Its help renderer keeps hints on one line and truncates them cleanly. Optional information columns disappear when they consume too much of a narrow row.

`model/ui.go` switches to compact layout below width or height thresholds and recalculates layout when the editor changes height. Styles use semantic palette fields in `internal/ui/styles/`; components receive the current style set. ANSI-aware width and truncation use `github.com/charmbracelet/x/ansi`, so Unicode and color escapes do not break cell measurements.

## Applying this in zlib

- Keep one owner for each page's focus, loading, and cancellation state. The existing shell/page models can do this without copying Crush's entire application model.
- Continue using shared book-list geometry for Search, Favorites, Recommended, Most popular, and History. Calculate from the viewport and hide secondary fields before truncating the title.
- Treat preview and save as separate actions in the theme picker. Cancel should restore the committed palette.
- Use `renderActionButton` for actions on forms, confirmations, settings, and download pages. Match Crush's filled, borderless focus treatment; keep list selection separate.
- Keep shell status and key hints inside terminal width. On small terminals, spend rows on the list and primary action before optional text.

Crush's `internal/ui` packages cannot be imported from this repository. Use the Charm libraries already in `go.mod` and adapt only the behaviors that fit zlib's smaller command set.

## Crush page inventory

Crush's top-level `UI` has four screen states (`internal/ui/model/ui.go`, `uiState` and `Draw`). Most other pages are dialogs layered over a screen. A dialog implements `ID`, `HandleMsg`, and `Draw`; `dialog.Overlay` routes input to the top dialog before the underlying page and draws it last (`internal/ui/dialog/dialog.go`). An asynchronous dialog may use `OpenDialogWithGrace` to absorb keys still arriving from the previous view.

| Page | Source | Structure and interaction | Lesson for zlib |
| --- | --- | --- | --- |
| Onboarding | `model/ui.go`, `dialog/models.go`, `dialog/auth_method.go` | Provider/model choice and authentication use a series of dialogs anchored near the lower left. | Keep login as a clear form with recoverable errors and an obvious way back. |
| Project initialization | `model/onboarding.go`, `initializeView` | Short prompt with two selectable buttons. | Present consequential choices as actual actions with visible focus. |
| Landing | `model/landing.go`, `landingView` | Working directory and selected model above LSP/MCP/skill information; the editor is ready for input. | zlib home should emphasize search and available commands, without agent status panels. |
| Chat, wide | `model/ui.go`, `Draw`, `generateLayout`; `model/chat.go` | Sidebar, message viewport, input, optional pills, and status occupy measured rectangles. | Keep list, selected-book card, and footer in measured regions. |
| Chat, compact | `model/ui.go`, `updateLayoutAndSize` | Sidebar yields to a header; session details can overlay the main area. Layout recalculates when editor height changes. | Hide optional book columns before compressing the title or action. |

### Dialog pages

| Page or family | Source | Interaction and layout |
| --- | --- | --- |
| Commands | `dialog/commands.go`, `commands_item.go` | Search filters the list; arrows wrap; Tab switches groups; resize rebuilds width-dependent items. Shortcuts hide uniformly when they crowd names. |
| Models | `dialog/models.go`, `models_item.go` | Filterable list with wrap navigation and model-type radio choices. Enter selects; a separate key edits authentication. |
| Sessions | `dialog/sessions.go`, `sessions_item.go` | Search, select, rename, and delete have explicit modes. Wheel scroll and double-click are supported. Timestamps hide when narrow; deletion is refused while busy. |
| Theme list, new theme, editor | `dialog/theme.go`, `theme_new.go`, `theme_editor.go` | Navigation skips section headers and wraps. Highlight previews; Enter commits; Esc reverts. Creation and editing have distinct pages. |
| File picker | `dialog/filepicker.go` | Bubbles filepicker returns a selected path. Image preview disappears if height is too small; file navigation remains. |
| Authentication | `dialog/auth_method.go`, `api_key_input.go`, `oauth.go`, `aws_sso.go`, `mcp_auth.go` | Choice cards, input, verification, waiting, success, and error are separate states. OAuth can cancel polling and report a recoverable failure. |
| Small settings | `dialog/reasoning.go`, `notifications.go`, `mcp_toggles.go` | Filterable choices or toggles, with current choice visible. The parent persists the returned action. |
| Permissions and questions | `dialog/permissions.go`, `question_yesno.go`, `question_single.go`, `question_multi.go`, `question_freetext.go`, `question_form.go`, `question_confirm.go` | Prompt-specific controls and explicit confirmation. A narrow view does not depend on split content. |
| Command arguments | `dialog/arguments.go` | Required values are checked before execution; a missing value leaves the form open with a warning. |
| Quit | `dialog/quit.go` | Two buttons support keyboard and mouse. A first click focuses; a second activates. Esc cancels. |

Shared controls live in `internal/ui/common/button.go` and `internal/ui/list/list.go`. Button appearance depends on selected, hovered, and inactive state; one renderer also provides a mouse hit map. List selection and viewport offset are separate. `List.Render` stops at the visible row budget, and individual items can cache expensive rendering. `completions/completions.go` wraps arrow navigation and scrolls selection into view. `dialog/common.go` sizes content after subtracting border and padding, reserves a scrollbar column only on overflow, and truncates help cleanly to one row. `model/status.go` keeps short notices in a fixed region.

## zlib TUI page map

This inventory covers the screens reachable from `zlib tui` and the interactive direct commands represented by `internal/cli/shell.go`. The last column is a review target, not a claim that every path has been exercised in a terminal. `docs/tui.md` remains the interaction contract.

| Page or flow | zlib source and current design | Crush pattern | Failure and recovery to check |
| --- | --- | --- | --- |
| Home and `/` menu | `shell.go`, `shell_palette.go`, `shell_home.go`: branded home, search input, centered command dialog | Landing and Commands: filtered list over the current page, visible focus, short command hints | The dialog preserves the page and selection; unknown commands keep the draft; a protected draft survives login. |
| Quit | `shell.go`: `/quit` exits the shell | Crush Quit dialog shows when exit needs confirmation | The current zlib command is immediate; document that behavior and keep cancellation on pages such as Doctor. |
| Login and logout | `shell.go`, `tui_inline_form.go`, `shell_confirm.go`: credentials and confirmation | Authentication states and Quit buttons | Mask password; preserve entered values after login error; cancellation keeps the session; failed removal stays visible. |
| Search form | `search_filter_form.go`: query, years, language and format pickers, exact match, sort, Search button | Question Form and dialog list: visible fields and focused choices | Empty query focuses the field; invalid years retain input. |
| Search results | `search_interactive.go`, `book_feed_view.go`: paged rows, selected card, book detail request | List: selection, viewport, and detail result are separate | Empty success differs from request error; retry targets the same page; late detail cannot replace the new card. |
| Favorites | `search_interactive.go`, `book_feed_view.go`: saved-book pages | Same list dimensions and card as Search | Preserve page and selection when retrying; avoid a different column layout. |
| Recommended | `search_interactive.go`, `book_feed_view.go`: feed through shared renderer | Same list contract | Loading, empty, and failure remain distinct. |
| Most popular | Same feed renderer | Same list contract | Same loading and retry behavior. |
| History | `history_interactive.go`, `book_feed_view.go`: selection, redownload, delete | Sessions: list mode and delete mode are separate | Delete failure retains item and retry; Cancel leaves history unchanged. |
| Profile | `profile_tui.go`: full-screen account and quota | Fixed controls plus scrollable content | Failed profile request offers retry or exit. |
| Doctor | `doctor_tui.go`: probe, available domains, current-domain marker | Async list; focused row differs from current setting | Retry cancels old probe; late result cannot replace newer scan; empty result says so. |
| Theme | `theme_picker.go`, `theme_editor.go`, `theme_custom.go`: filter, preview, create, edit, save | Crush Theme, ThemeNew, and ThemeEditor: copy a base, edit colors, preview on focus, save explicitly | Built-ins stay intact; custom files load by name; cancellation does not write; narrow screens remain usable. |
| Settings home/sections | `settings_model.go`: Network, Downloads, Kindle | Small setting lists with values as secondary text | Show source/override, validation error, and save result in place. |
| Network/domain | `settings_model.go`: domain probe/selection, proxy edit/test | Async list and short status notice | Failed probe or save keeps previous domain; current domain remains identifiable. |
| Download settings | `settings_model.go`: default directory and retries | Edit/action modes | Invalid retry count stays editable; browser opens at selected directory. |
| Kindle settings | `settings_model.go`, `shell_kindle.go`: recipient, sender, SMTP host and port | Form and settings actions | Validate before saving; never display SMTP password. |
| Download confirmation | `search_interactive.go`, `history_interactive.go`, `confirmation.go` | Quit/permission confirmation | Esc returns to list; transfer starts only after confirmation. |
| Save-to directory | `download_directory.go`: use current or browse/edit for this download | File picker with primary path visible | Invalid directory stays editable; cancellation does not start transfer. |
| Download progress | `download.go`: progress and result | Fixed status space for progress and errors | Failure keeps a readable reason and next action. |
| Kindle file picker and send | `tui_filepicker.go`, `kindle.go`: browse, confirm, mail progress | File picker plus confirmation | Cancel before send; report SMTP failure; stopping a wait may not recall sent mail. |
| Direct-command TUI entry | `search_interactive.go`, `tui_form.go`, `tui_inline_form.go`, `download_directory.go`, `tui_filepicker.go`: direct commands may open the same screen models or an interactive setup form | Crush uses the same dialog components from multiple entry points | Verify TUI behavior from both `zlib tui` and direct commands; keep explicit JSON/text output suitable for pipes. |

The five book lists should continue to share card placement, row spacing, columns, and compact behavior. That requirement is specified in `docs/tui.md#book-list-spacing` and implemented by `book_feed_view.go`.

### Home, login, and command input

`shell.go` routes a command to a page model, then gives that page a terminal-sized viewport. Crush's central `UI.Update` serves the same routing role. zlib's pages are full Bubble Tea models; Crush's dialogs are smaller stateful components. The useful common rule is input ownership: when `/` completion is open, arrows select commands; after it closes, the search field receives typing. When a confirmation opens, its keys take precedence over the list behind it. Crush uses the same order for dialogs and editor focus.

The signed-out path opens login immediately. An invalid credential or unavailable domain should remain attached to the login form; a short status message at home would lose the entered context. The logout confirmation starts on Cancel, matching the safe-default behavior of Crush's Quit dialog. `shell.go` also keeps a protected command draft so a successful login can resume the user's original action. The full home state must remain clear after either successful login or logout.

### Search and the five book lists

The Search form presents every filter together. `search_filter_form.go` uses a Bubbles text input, a compact field list, and centered pickers for language, format, and sort choices. The pickers own arrow and Space keys while open; the form keeps the other values. The Search button is filled on focus without a border, consistent with `internal/ui/common/button.go`.

`search_interactive.go` has loading, results, inline query editing, and error states. Up/down or j/k changes a row; PgUp/PgDn, Home/End, and left/right work at different scopes. Enter opens confirmation. In an interactive direct search, `/` edits the existing query; in the shell, Ctrl+F is used. Search draws results in one frame after the request completes and anchors controls to the bottom in loading and empty states. `book_feed_view.go` renders the selected-book card and columns shared by Search, Favorites, Recommended, Most popular, and History. This shared renderer is the main protection against spacing drift.

The selected book's detail request is separate from the list request. The card can update immediately from list data while additional metadata loads. A later result must be checked against the selection that requested it. Crush's list also separates selection from viewport offset, which is why scrolling a selection within visible rows need not jump the whole page. For narrow terminals, first drop optional metadata columns, then truncate title or author by ANSI cell width. Render only the visible rows and keep a stable gap around the card.

Favorites has EAPI pagination; Recommended and Most popular are feeds displayed through the same list model. Their empty messages should name their source. A single failed request should show its source and retry key; it should not be rendered as a successful empty list. The selected download action uses the same confirmation everywhere so Enter has one meaning across book lists.

### History and confirmation

History is a book list with two secondary actions: redownload and delete. `history_interactive.go` has distinct states for results, delete confirmation, deletion in progress, and list loading. The delete dialog begins with Cancel focused. While deleting, list navigation is suspended; a failed deletion keeps the record and offers another attempt. This corresponds to Crush's Sessions dialog, where normal, renaming, and deleting are explicit modes. A confirmation should preserve the current row and viewport so Cancel returns to the same context.

`confirmation.go` draws a small overlay over the current page. The underlying page remains recognizable, while the action and Cancel buttons are the only current targets. Crush's overlay stack provides the same input priority; its `OpenDialogWithGrace` is useful when a dialog opens from an asynchronous event, since a late Enter key could otherwise confirm an action the user has not seen. zlib's book confirmations currently open from a direct keypress, so the grace delay is a pattern to evaluate only if asynchronous confirmations are added.

### Profile, Doctor, theme, and settings

Profile uses a viewport for account fields and a width-aware Bubbles progress bar (`profile_tui.go`). The EAPI used count and limit appear once above the bar; the derived remaining count is omitted. Wide terminals show account fields in two columns. It shows a spinner before load, a readable error with `r` to retry, and a scroll percentage only when content exceeds the viewport. Crush's Section and Status components inform the section lines and fixed controls while content scrolls. The quota state distinguishes unavailable data from zero remaining downloads.

Doctor runs a domain probe (`doctor_tui.go`). It shows a spinner, then only available domains. The focused row is navigation state; the `Current domain` marker identifies configuration state. The two must remain independent. The current available domain receives initial focus. `r` cancels the old probe, increments a sequence, and starts a new one; a reply with an older sequence is ignored. Esc also cancels. This is the clearest existing zlib example of the async message rule from Crush's `internal/ui/AGENTS.md`.

The Theme picker (`theme_picker.go`) filters names while the highlighted entry previews a palette. The page's chrome uses the committed palette; the preview region uses the candidate palette, including its background and selected-row surface. Ctrl+N copies the highlighted palette into a new named theme; Ctrl+E edits a custom theme or starts a copy of a built-in. `theme_editor.go` validates `#RRGGBB` fields before Ctrl+S writes a theme file. Enter selects a saved palette; Esc cancels without changing it. Crush's Theme, ThemeNew, and ThemeEditor dialogs separate these actions. Every preview cell has an explicit background so terminal colors do not leak through.

Settings (`settings_model.go`) contains home, Network, Downloads, Kindle, domain results, directory browse, and edit states. The home page groups settings by task. A selected setting opens an editor, while Tab moves among Save, Cancel, Reset, and context actions such as Browse or Test. The editor uses a visible Bubbles input and borderless action buttons based on Crush's form and button patterns; narrow terminals wrap the buttons. The Proxy URL editor shows the full value, while settings lists redact embedded credentials. Esc from a dirty editor opens a discard confirmation. Domain and proxy checks use a spinner and cancellation; changing the proxy invalidates an in-flight test. Size editor text to the content area and keep actions visible on narrow screens. Do not confuse a domain being focused with the domain being saved.

### Download and Kindle

`download_directory.go` offers the current directory first, with a separate path or browse route for this download. The user should see the resolved directory before confirming. `download.go` has fetch, transfer, completion, and cancellation states. Progress uses transferred bytes and known total when available. Esc or Ctrl+C cancels the transfer context; the view reports cancellation and incomplete-file cleanup. In the shell, completion stays on the download page with the full saved path and borderless Back / Send to Kindle actions. The Kindle action opens a confirmation; cancel, preflight failure, and send result return to the saved-file page without restarting the download. Long paths wrap and scroll. A download failure opens a full-screen error page with the book and a URL-safe reason. `r` retries the same book and directory in a new request generation; Esc returns to the selected list item. Kindle send failures keep the file and reason visible but require a new explicit send, since a failed SMTP response does not prove the message was not delivered.

Kindle has a file picker (`tui_filepicker.go`), a confirmation overlay, a send page (`kindle.go`), and SMTP settings (`settings_model.go`). The picker shows its current directory and allows path entry. Confirmation shows the chosen filename before sending. Crush's filepicker demonstrates a useful priority: hide optional previews in short terminals while leaving file navigation and selection intact. For mail, a stopped wait does not prove that a message was never delivered; the UI and diagnostic text should retain that distinction.

## Error handling and logs

Crush converts an error into a typed `InfoMsg` through `internal/ui/util/util.go` (`ReportError`). `model/ui.go` receives it, writes errors through `slog`, puts the notice in `model/status.go`, and schedules a clear message after its TTL. Warnings, success, and ordinary information use the same route. The status renderer truncates long notices to terminal cells. A form error that the user can correct stays in its form; `dialog/arguments.go` keeps a missing required argument visible. `dialog/oauth.go` stops polling after a failure and presents an error state that can be dismissed or retried. These are different treatments for transient status and recoverable input.

`internal/log/log.go` writes structured `slog` JSON with source locations. Its normal level is Info; debug raises it to Debug. It configures 10 MB rotation and a 30-day max age. The configured data directory contains `logs/crush.log` (`internal/cmd/root.go`); the default project data directory is `.crush` (`internal/config/config.go`). Server mode can use a separate cache path (`internal/cmd/root.go`, `internal/cmd/server.go`). `internal/cmd/logs.go` provides `crush logs`, `--tail`, and `--follow`; the README documents `crush --debug` and the `debug` and `debug-lsp` options.

The debug HTTP transport in `internal/log/http.go` records method, URL, elapsed time, status, headers, and bodies. It redacts header names containing authorization, API key, token, or secret. **The source does not redact request or response bodies**, and query parameters can also carry sensitive values. This transport should not be copied into zlib for authenticated EAPI calls without body and URL sanitization. Review logs before attaching them to a public issue.

For zlib, each request screen needs separate loading, empty-success, and recoverable-error states. An error should name the operation, such as `Could not load page 2` or `Could not save settings`; retry should use the same query, page, proxy, and domain. On exit, cancel in-flight work where possible and reject late messages. A useful bug report records command/page, version, OS and terminal, domain or proxy *presence*, HTTP status or stable EAPI error code, and visible failure. Keep credentials, cookies, SMTP passwords, signed URLs, and raw download payloads out of logs and screenshots. `docs/troubleshooting.md` is the user-facing diagnostic guide.

| Failure class | Crush evidence | zlib page and useful response | Evidence to collect without secrets |
| --- | --- | --- | --- |
| Invalid local input | `dialog/arguments.go` marks missing required fields and leaves the dialog open | Search query, settings value, Kindle address: focus the field and explain the rule | Field name and validation message |
| Remote request failure | `util.ReportError`, `model/status.go` surface a short notice; `dialog/oauth.go` keeps an error state | Search, Favorites, feeds, History, Profile, Doctor: show the failed operation with retry or back | Page/command, domain, HTTP status or stable EAPI error code, time |
| Empty successful result | Crush lists render an empty collection separately from an error | Search, feeds, History, Doctor: say no books, history, or available domains were found | Query/page and whether the request succeeded |
| Stale async result | `model/ui.go` and its subcomponents use generation checks for work that outlives a view | Book detail, Doctor probe, settings proxy test: reject results from an earlier selection or run | Request sequence, selected item, page transition |
| Cancelled or incomplete action | `dialog.Overlay` keeps Cancel in the active modal; OAuth stops polling on error/exit | Download and Kindle: show what was stopped and what may have completed | Stage, bytes written for download, final file presence; never mail credentials |
| Unexpected application failure | `internal/log/log.go` stores structured records; `crush logs --tail` reads them | Add a zlib log only if sanitized and documented; include a stable error type and source | Version, OS, terminal, command/page, sanitized error record |

Troubleshoot from the visible state first. For a reproducible zlib list failure, record the page and requested page number, then retry once with the same settings. For a domain failure, compare `zlib doctor` with the configured domain and proxy. For a save failure, verify the active configuration source before changing values. If a future zlib log command is added, it should report the log path, support bounded tailing, and avoid reading secrets into the TUI.

## Review order for zlib

1. Keep measured header, card/list, action, and help regions. Remove status text that repeats what focus already shows.
2. Keep selection, scroll offset, card, and width decisions in the shared book-list renderer. Hide optional columns before truncating title or author.
3. Use the same action words and focus behavior for Search, Save, Download, Send, Delete, Cancel, and retry. A filled action button need not have a border.
4. Associate asynchronous results with their request, cancel on exit, reject stale replies, and retain enough input to retry.
5. If structured logging is added to zlib, document its location and sanitize EAPI, cookie, download, and SMTP data before writing it.
