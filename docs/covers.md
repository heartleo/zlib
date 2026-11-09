# Book covers

The selected-book card and Book details (press `i` in a list) show the cover on
the left, drawn as a real image. This needs a terminal that supports the Kitty
graphics protocol or Sixel. On any other terminal covers are hidden: the card
is text only, with no empty column, no placeholder, and no cover requests.

## Supported terminals

| Protocol | Detected by |
| --- | --- |
| Kitty graphics | Reply to a Kitty graphics query |
| Sixel | DA1 attribute 4 |

Both also need the terminal to report its cell size, which zlib uses to fit the
image. If both protocols are available, Kitty is used.

Detection is by query, not by terminal name. Terminals that implement Kitty
graphics include Kitty, WezTerm and Ghostty; Sixel is implemented by foot,
xterm and WezTerm, among others. See the
[Kitty graphics](https://sw.kovidgoyal.net/kitty/graphics-protocol/) and
[XTerm control sequences](https://invisible-island.net/xterm/ctlseqs/ctlseqs.html)
references.

Covers appear a moment after the screen opens, when the terminal's reply
arrives. Until then the card is text only.

## When covers are hidden

| Situation | Result |
| --- | --- |
| No Kitty or Sixel reply, or no cell size | No covers |
| Terminal under 100 columns or 20 rows | No covers |
| Dialog or command palette open | Image hidden until it closes |

Replies to the terminal queries must reach zlib. Over SSH, inside tmux or
screen, or through a multiplexer that drops them, zlib sees no protocol and
hides covers.

## When a cover is missing

| Situation | Card shows |
| --- | --- |
| Cover is loading | A single animated spinner, with no text or border |
| Book has no cover | `no cover` |
| Request failed or format unsupported | `cover unavailable` |

A failed cover is not retried on every redraw; reopen the list to try again.

## Limits

- JPEG, PNG and GIF decode; GIF shows its first frame. Other formats, such as
  WebP, show `cover unavailable`.
- Images are limited to 2 MiB, 4096 pixels per side and 4 million pixels.
- Requests use HTTPS only, send no account cookies or headers, follow at most
  five redirects that stay on HTTPS, use the configured proxy, and time out
  after 6 seconds.
- Sources larger than the card are downscaled to fit; smaller ones are not
  enlarged.

## Prefetch and cache

- The selected cover loads first, then up to four neighbors on each side, with
  at most eight requests in flight. Download history also loads the details of
  nearby rows, because history rows carry no cover URL until then.
- Each list keeps up to twelve decoded covers or eight million pixels in
  memory. The least recently used cover is dropped first; the selected one stays.
- Downloaded covers are also stored in the user cache directory under
  `zlib/covers`, up to 300 files or 96 MiB, least recently used first. Entries
  are checked again on every read and deleted if they no longer decode. Delete
  the folder at any time to clear it.
