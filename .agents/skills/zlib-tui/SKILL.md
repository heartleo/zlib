---
name: zlib-tui
description: Change zlib's Bubble Tea terminal screens, book cards, list spacing, or shell navigation while preserving shared layout and selection behavior.
---

# zlib TUI changes

Keep interactive CLI and shell entry points on shared models. `profile` uses
`profilePageModel`; Kindle uses `runStandaloneShellCommand` for file selection,
configuration, confirmation, password setup, and send results. Standalone pages
exit to the terminal on return. Preserve JSON and non-interactive send behavior.
Check `TestStandaloneKindleSharesShellPages`, `TestStandaloneKindlePasswordAndResult`,
and `TestStandaloneProfileMatchesShellPage` when changing these flows.

Home has no search input or Enter-to-search hint. Open commands with `/`;
ignore ordinary typing, paste, and Ctrl+K on idle home. Esc closes the command
list and restores home without an input. Keep the signed-out login shortcut.
Check `TestShellHomeRequiresSlashForCommands` when changing home input handling.

Use `renderActionButton` from `tui_layout.go` for every operation on a form,
dialog, or result page. Its idle state uses the theme surface and title color;
focus uses a filled accent, or error color for a destructive action. Keep two
cells of horizontal padding, no border, and no `>` prefix. Wrap action groups
with `actionButtonRows` at the available width. Color gaps between dialog
buttons with the panel background; plain spaces can expose the terminal color
after an ANSI reset. List and folder rows keep
`selectionRow`. Check `TestActionButtonsShareConfirmationStyle` after changing
button rendering.

For the theme picker, keep the list and filter in the current palette while the preview follows the highlighted name. Paint component backgrounds, including cursor and separator cells. Check `TestThemePickerColorsFollowSelection` for all palettes at compact and wide sizes; preview and cancellation must not write preferences.

Custom themes live in the user config `themes` directory. Ctrl+N copies the selected palette into a named file; Ctrl+E edits custom colors or starts a copy of a built-in. Validate names and `#RRGGBB` values before saving. Ctrl+S writes the file; Esc discards draft edits. Keep the selected-book preview row on the candidate theme's surface, with every preview cell painted. Check `TestCustomThemeCreateEditAndReload`, `TestCustomThemeValidationAndCancel`, and `TestThemeEditorFitsAndKeepsPaintedBackground`.

Compare against a local clone of [charmbracelet/crush](https://github.com/charmbracelet/crush) (`<crush-checkout>`). Check its current HEAD before relying on an old revision cited in docs. Browse `internal/ui/` for layout and component patterns. Its `internal` packages cannot be imported by zlib; use the Charm packages already in `go.mod`.

Read [the TUI layout rules](../../../docs/tui.md#book-list-spacing) before changing book lists or cards. `book_feed_view.go` renders the shared Search, Favorites, Recommended, Most popular, and History list; keep its row calculation and rendered height aligned at compact and expanded terminal sizes.

Search results appear together when a request finishes; do not bring back row-reveal animation. Loading, empty, and failed Search states keep controls at the bottom. Empty results show the page and filter action. Check `TestSearchResultsAppearTogetherAndSelectionStaysStable` and `TestSearchLoadingAndEmptyKeepControlsAtBottom`.

The Search form in `search_filter_form.go` shows query, year range, language, format, exact match, and sort order together. Long choices open a centered picker; arrows navigate and Space toggles multiple choices. Ctrl+S submits; invalid years keep the form open with the error directly below the field. Filter rows use a fixed label column under a `sectionHeader("Filters", …)` rule; do not pass aligned label text through `selectionRow`, which collapses spaces. The query box needs 20 rows; the form caps at 100 columns. The result page reopens the same form with current filters and must resize it through `tea.WindowSizeMsg` so the input width matches the terminal. Loading, result status, and empty states show `searchFilterChips`; the status line drops them when fewer than 12 cells remain. Check `TestSearchFilterFormAlignsLabelsAndShowsErrorsAtField`, `TestSearchFilterFormQueryBoxFollowsTerminalSize`, `TestSearchStatesShowActiveFilters`, and `TestSearchRefineSizesQueryToTerminal`. Keep `SearchOptions`, `searchEAPI`, direct `search` flags, and the form mapped to the same EAPI fields. Check `TestSearchFilterFormSelectsEveryEAPIConstraint` and `TestSearchEAPISendsAllFilters`.

Book details come from EAPI for the selected `id:hash`. Fetch after selection settles, cache by reference, and keep late results tied to their original book. Show fields the response provides; the Android model does not guarantee categories or separate ISBN fields. See [EAPI notes](../../../docs/eapi.md#current-scope) for the evidence boundary.

Check the shared layout and selection tests in `internal/cli/book_feed_view_test.go` and the EAPI mapping tests in `internal/zlib/eapi_book_test.go`. When changing shared layout code, run `go test ./...` and `go vet ./...` after the focused test passes. Update `docs/tui.md` when interaction or spacing rules change.

Keep recoverable failures on the page that owns the request. Download failure shows the book, a URL-safe reason, and `r` to retry with the same directory; retry must start a new shell generation so late messages are ignored. Kindle send failure shows the file and reason but has no automatic retry because SMTP delivery may have completed. Keep login and setting validation on their forms. Check `TestShellDownloadFailureKeepsContextAndRetries` and `TestShellKindleFailureRequiresExplicitResend` when changing these paths.

In the shell, a completed download stays on its page with the absolute saved path and Back. Show Send to Kindle only when the saved file extension is in kindleAllowedTypes; keep hidden actions unreachable by keyboard. The Kindle action confirms before SMTP; cancel, missing settings, success, and failure return to the saved-file page without restarting the download. Wrap and scroll long paths; keep the standalone CLI download exit behavior. Check `TestShellDownloadReturnsToSelectedBook`, `TestShellDownloadKindleActionReturnsToCompletion`, and `TestDownloadCompletePathWrapsAndWaitsForReturn`.

When Kindle SMTP password is missing after confirmation, open a masked form. Save `ZLIB_SMTP_PWD` to the global `.env`, set it in the current process, then continue the same send. Cancel returns to the saved file; a write error stays on the form. Check `TestShellKindlePasswordSaveFailureKeepsMaskedForm` and `TestSettingsSaveSMTPPasswordWithoutChangingOtherKeys`.

The Doctor shell page uses Bubbles list, spinner, and the shared help renderer, following the local Crush command-list pattern. Keep its results limited to domains accepted by `availableDomains`. Show one domain per row, use the accent border for the focused row, and mark the current configured domain separately. Retry and page exit must cancel the prior probe context. Check `TestDoctorPageListsOnlyAvailableDomains` and `TestDoctorPageRetryRejectsOldResultsAndCancelsOnExit` after changing it.

Profile shows the EAPI daily used count and limit once, above a width-aware horizontal progress bar. Keep unknown quota distinct from zero used, use two account columns on wide terminals, and show scroll controls only when content overflows. Check `TestProfileQuotaBarUsesWidthAndDoesNotRepeatRemaining` and `TestProfilePageUsesFullScreenAndScrolls`.

For shell home changes, keep the original five-row letterforms in `shell_home.go`, painted by `homeWordmark` with one column-wise Accent-to-Link gradient (`wordmarkRampEnd` substitutes Info or Success when Link is too close to Accent). Show nothing else with it: no brand text, build version, side strokes or trademark sign. Signed-in command hints use muted descriptions; the signed-out home shows a heading, a focused `renderActionButton("Log in")`, and the sign-in method only. Check `TestHomeWordmarkGradientVisibleInEveryTheme`. Keep the logo free of side strokes. The hints name commands registered in `shellCommands`; typing `/` opens the selectable palette. Without a saved session, shell startup opens the login form directly; Esc reaches the signed-out home, which shows the login action and method. Keep Enter available there on compact terminals. After logout, open the login form again. Do not put session status or the working directory below the logo. Check `TestShellHomeLogoAndShortcutsResponsive`, `TestShellSignedOutHomeOpensLogin`, and `TestShellStartsOnLoginWithoutSession`.

The `/` and Ctrl+K command list is a centered overlay in `shell_palette.go`. Keep its filter and selected row inside the dialog; the underlying page must retain its viewport and selection. At 80×24, show five commands with descriptions below their names; narrow terminals use one-line rows. Arrow keys wrap, Tab completes into the inline input, Enter runs, and Esc restores a saved search draft. Fit the dialog at 80×24 through 20×5. Check `TestShellCommandDialogOverlaysPageAndKeepsFocus` and `TestShellCommandDialogFitsCompactTerminals`.

Shell command feedback has three tones: `setError` (✗, error color, stays until the next key press), `setSuccess` (✓, success color) and `setNotice` (neutral hint); the last two expire after five seconds. Render it on the title row (row 0), never on row 1, which holds the list status line. Keep it above the input and key hints, including while the command dialog is open. Keep actionable form and request errors on their owning page. Check `TestShellErrorNoticeStaysUntilKeyPressWithoutReplacingInputOrHelp`, `TestShellInfoAndSuccessNoticesExpire`, `TestShellNoticeKeepsListStatusVisible` and `TestShellThemeSavedNoticeLeavesFooterAvailable`.

The download-directory choice and folder browser use a centered dialog over the selected book list. The displayed directory is the default action; the browser reopens at the current selection. Keep both direct-command and shell entry points on the same model. Check TestDownloadDirectoryDialogKeepsBookListBehindIt and TestDownloadDirectoryDialogAcceptsTypedFolder.
