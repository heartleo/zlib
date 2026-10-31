# Improving zlib's TUI with Lessons from Crush

Design recommendations from Crush's TUI source at revision `b7b5522585e7302d2ee19b1a74a7003ac29ea562`.

## What to do first

Fix list height and the narrow layout first, then add a details preview, error retry, and returning to the list after a download. zlib already has spinner, textinput, progress, huh forms and multiple themes; the main gap is continuity of interaction between these components.

| Priority | How Crush does it | Current zlib problem | Recommendation and cost |
| --- | --- | --- | --- |
| P0 | [List](https://github.com/charmbracelet/crush/blob/b7b5522585e7302d2ee19b1a74a7003ac29ea562/internal/ui/list/list.go) manages width and height, visible offset, selection and a render cache | The search model only records width and renders every result line; the recommended list can reach 100 items | Record terminal height, pin the title and help area, clip the list to the available rows and keep the selection visible. Medium cost |
| P0 | The [main UI](https://github.com/charmbracelet/crush/blob/b7b5522585e7302d2ee19b1a74a7003ac29ea562/internal/ui/model/ui.go) switches to a compact layout by width and height | Fixed column widths wrap in narrow windows; the ID column is only 10 characters and truncates the full reference | Show more columns when wide; keep title, author and format when narrow; put the full reference in details. Choose breakpoints from zlib's content instead of copying Crush's 120×30. Medium cost |
| P1 | [Dialog/Overlay](https://github.com/charmbracelet/crush/blob/b7b5522585e7302d2ee19b1a74a7003ac29ea562/internal/ui/dialog/dialog.go) isolates overlay input from the main screen | Enter downloads immediately; there is no details preview | Add a details action: right pane when wide, separate details page when narrow; Esc returns and keeps the cursor. Enter can keep downloading so existing keys do not change without notice. Medium cost |
| P1 | [Status](https://github.com/charmbracelet/crush/blob/b7b5522585e7302d2ee19b1a74a7003ac29ea562/internal/ui/model/status.go) combines help with time-limited messages | A failed fetch exits; choosing a book exits the list and then starts the downloader | Keep query, page and cursor in a parent model; return to the list after a download and show the saved path. Keep errors on screen with retry and back. Medium to high cost |
| P1 | Status uses `help.Model` and `help.KeyMap` | Help text is assembled by hand; empty results return early and hide the still-usable search entry | Drive both real keys and hints from `key.Binding`; keep short help visible and expand it with `?`. Keep search, retry and quit on empty results. Low cost |
| P2 | [FilterableList](https://github.com/charmbracelet/crush/blob/b7b5522585e7302d2ee19b1a74a7003ac29ea562/internal/ui/list/filterable.go) fuzzy-filters loaded items | Recommended and favorites lists have no filter; `/` in search only changes the query | Offer local filtering for the recommended list; when filtering only the loaded page of favorites, say so. Remote search still sends a request only on confirmation, so EAPI is not hit on every keystroke. Medium cost |
| P2 | [Actions dialog](https://github.com/charmbracelet/crush/blob/b7b5522585e7302d2ee19b1a74a7003ac29ea562/internal/ui/dialog/actions.go) and [commands dialog](https://github.com/charmbracelet/crush/blob/b7b5522585e7302d2ee19b1a74a7003ac29ea562/internal/ui/dialog/commands.go) | Search, favorites and history are separate entry points | Once the parent model is stable, offer a menu to switch lists and act on a book. Do not bring in a whole command system for a few actions. Medium cost |

## Where it lands in zlib

- [search_interactive.go](../internal/cli/search_interactive.go): `searchSelectModel` is shared by search, recommended, popular and favorites. `WindowSizeMsg` only stores Width; `View` iterates over every book. Unify height, scrolling and responsive row layout here first.
- [history_interactive.go](../internal/cli/history_interactive.go): history has its own model, which also renders every line and exits on error. Reuse the list rendering and key logic, keep history's own data loading.
- [download.go](../internal/cli/download.go): already has a progress bar, cancellation and non-terminal output. Interaction improvements belong in the parent model's state transitions; keep the direct download path for scripts and agents.
- [eapi_book.go](../internal/zlib/eapi_book.go): `FetchBook` reads details from `/eapi/book/{id}/{hash}`. The mapping only builds the download path and does not call `/file`, so it can back a details preview.
- [theme.go](../internal/cli/theme.go): semantic colors and multiple themes already exist. Keep using them; there is no need to copy Crush's palette first.

Load details per selected item and cache them. Carry the book reference or a request sequence number with each async response and ignore stale ones, so old details cannot overwrite a newer selection when the cursor moves quickly. Do not request details for all 100 books of the recommended list at once, and never request a download link for a preview.

Viewport scrolling and EAPI pagination are two layers: up and down move within the current page, left and right request the previous or next server page. Recommended and popular have no server pagination. Local fuzzy filtering must not be presented as a full-library search.

## Dependencies and reuse limits

Crush ([go.mod](https://github.com/charmbracelet/crush/blob/b7b5522585e7302d2ee19b1a74a7003ac29ea562/go.mod)) and zlib ([go.mod](../go.mod)) both build on Bubble Tea, Bubbles and Lip Gloss v2.

Crush's `internal/ui` is covered by Go's internal import rule and cannot be imported from zlib. Its [license](https://github.com/charmbracelet/crush/blob/b7b5522585e7302d2ee19b1a74a7003ac29ea562/LICENSE.md) is FSL-1.1-MIT and must not be copied as if it were plain MIT code.

Bubbles `list`, `viewport`, `help` and `key` cover the first round. Viewport clipping is simpler than Crush's cached lazy list, and zlib's list sizes do not need the latter.

## Order of work and acceptance

1. List height, narrow layout and help: at 80×24, 120×30 and narrower windows, the title, current item and help stay visible; a 100-item list scrolls to the end; Chinese text and long titles do not break the layout.
2. Details and error recovery: Esc returns to the same page and position; fast switching never mixes up details; a timeout can be retried; empty results let the user change the query.
3. Keep browsing after a download: both success and cancellation return to the list; Ctrl+C still cancels reliably; consecutive downloads never start the same request twice.
4. Add local filtering and the actions menu last.

Verification covers window resizes, page boundaries, empty results, network errors and cancellation, and keeps the behaviour of `--json`, redirected output and the non-interactive agent path.
