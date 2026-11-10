# Send to WeRead

[README](../README.md) · [Configuration](configuration.md) · [Kindle](kindle.md)

zlib imports local EPUB, PDF, MOBI, AZW3 and TXT files into the bookshelf of
your WeRead account. The book then appears in the WeRead app on
your phone, the same way as an upload through WeRead's web "transfer" page.
Importing a local file does not require a Z-Library session.

## Before you start

WeRead has no public upload API. zlib signs in as the WeRead e-ink reader app and
uses that app's unofficial endpoints. Keep these limits in mind:

- The endpoints can change without notice and stop working until zlib is updated.
- WeRead may ask for verification ("risk control"). zlib cannot complete it;
  open WeRead on your phone, follow any prompt there, and retry later.
- Without a paid membership, WeRead refuses an import once the shelf would hold
  more than 200 imported books; removing imported books frees room. WeRead can
  apply further limits of its own, such as a number of imports in a period, and
  then shows its own message. A failed import may still count, so check a file
  before importing it.
- WeRead may list the login as an e-ink device.

| Limit | Default |
| --- | --- |
| Formats | EPUB, PDF, MOBI, AZW3, TXT |
| File size | TXT up to 20 MiB; other formats up to 300 MiB |
| Imported books | 200 on the shelf without a paid membership |
| Shelf | 500 books without a paid membership |

WeRead sets these on its server and can change them. The settings account page
(`zlib config weread account`) lists the accepted formats.

Use your own account and only files you are allowed to read.

## Log in

```sh
zlib weread login
```

A QR code appears in the terminal. Scan it with WeChat and confirm the login on
your phone. The code is square and fits a standard 80×24 terminal; in a wider
terminal the steps appear beside it and mark each one done as you go. The code
expires after five minutes; if it expires or you decline the login, press `r`
for a new code. A terminal too small for the code says how large it must be and
shows no part of it, because a clipped code cannot be scanned. zlib saves the login to `~/.config/zlib/weread.json` and
refreshes its access token automatically. Logging in again keeps the same device.

WeRead can stop accepting a saved login, for example when it expires or when
you sign in to WeRead on another device. In the terminal interface, an import
that hits this shows **WeRead login expired**: press `Enter` to scan a new QR
code, and the import of the same file continues once you are logged in. Press
`Esc` to go back instead. Commands such as `zlib weread import` print the error
and the login command, and exit with code 3.

You can also log in, log in again or log out from the settings:
`zlib config weread account`, or **WeRead account** in `zlib config` and `/config`.

Without a terminal, the QR code still prints to stdout and the command waits for
the confirmation. Delete the saved login with `zlib weread logout`.

After login, zlib fetches your WeRead nickname and shows the account as
`<nickname> (<user ID>)`; until the nickname is known it shows the user ID alone.
The account page in settings and `zlib weread whoami` list the nickname, user ID
and the file the login is saved to. Check
the account, and refresh the saved nickname, with:

```sh
zlib weread whoami
zlib weread whoami --json   # {"vid":"<id>","name":"<nickname>"}
```

The settings account page refreshes the nickname each time it opens. If no
nickname appears, `zlib weread whoami --keys` prints only the field names of
WeRead's reply, without any values, so you can report them safely.

## Import a file

```sh
zlib weread import ./book.epub
```

zlib checks the format and size before anything is uploaded. In a terminal the
confirmation names the file, its size and the account it goes to (or that a
WeChat login comes first). The progress page then shows three steps:

1. **Prepare upload**: WeRead issues an upload slot.
2. **Upload file**: a progress bar with bytes sent.
3. **Add to shelf**: WeRead reads the book; zlib checks every 2 seconds, up to
   90 times.

Esc cancels. During the upload nothing reaches WeRead. After it, zlib asks
WeRead to drop the upload and waits for the answer; press Ctrl+C to stop waiting,
in which case the book may still appear. The result names the book ID and the
account. When WeRead already has the same file, it reports **Already in WeRead**
and keeps the existing copy instead of adding another; if it is not on your
shelf, search for it in the WeRead app.

Without a terminal, or with `--json`, it imports directly and prints one line per
step on stderr. `--json` prints one document on stdout:

```json
{"path":"/home/me/books/Dune.epub","name":"Dune.epub","book_id":"<book-id>","already_existed":false}
```

`zlib weread` and `zlib weread import` without a file open a file picker that lists
only folders and EPUB, PDF, MOBI, AZW3 and TXT files, starting in your
default download directory (the working directory when none is set). In the
shell, use `/weread`, `/weread import <file>`, `/weread login` and
`/weread logout`. If no login is saved, the shell opens the QR page first and
continues the import afterwards.

Files with DRM cannot be imported.

## Download and import in one step

Search for an EPUB, then download it with `--send-to-weread`:

```sh
zlib search "dune" --ext epub --json --count 10
zlib download "id:hash" --send-to-weread
```

`--send-to-weread` works on `download`, `history --download`, `search`,
`favorites`, `recommended` and `most-popular`, and can be combined with
`--send-to-kindle`. In the TUI, a finished download of a supported file shows
**Send to WeRead** next to Back.

After a TUI import, the saved-file page reports the outcome next to its actions.

With `--json`, the download document gains a `weread` object:
`{"imported":true,"book_id":"..."}` (with `"already_existed":true` when WeRead
already had the file), or `{"imported":false,"error":"..."}` when the book was
saved but the import failed. The command then exits with code 5; do not
download the book again to retry the import, run `zlib weread import <path>`.

## How the import works

1. zlib asks WeRead for a temporary upload credential for the file name. Import
   limits are decided here.
2. It uploads the file to WeRead's Tencent Cloud Object Storage bucket with a
   signed request.
3. It tells WeRead the upload is complete and asks again until WeRead reports
   the book as added, already present, or unreadable. Cancelling at this step
   tells WeRead to abandon the import.

[WeRead import limits](weread-research.md) records the protocol and limits in
detail.

## Proxy

`ZLIB_PROXY` applies only to Z-Library. WeRead requests use the system proxy
(`HTTPS_PROXY`) unless you set **WeRead proxy**:

```sh
zlib config weread proxy
```

Enter `direct` to bypass every proxy, which helps when a global proxy routes
traffic abroad and WeRead asks for verification, or a proxy URL. The editor's
Test action (Ctrl+T) checks that WeRead answers without logging in. The value is
saved as `ZLIB_WEREAD_PROXY` in the global `.env`.

## If the import fails

| Symptom | Check |
| --- | --- |
| `not logged in to WeRead` (exit 3) | Run `zlib weread login` |
| `WeRead session expired or was revoked` (exit 3) | WeRead no longer accepts the saved login. Run `zlib weread login` again, then repeat the import. In the terminal interface, press `Enter` on the **WeRead login expired** page |
| `WeRead requires verification in its official app` | Open WeRead on your phone, complete any prompt, wait, then retry. If you use a proxy abroad, try `direct` as the WeRead proxy |
| `WeRead is unreachable` or a timeout | Check the WeRead proxy setting with its Test action |
| `WeRead did not confirm the import` | The upload finished but WeRead's answer was lost, it was still reading the book after 90 checks, or a cancellation could not be sent. Check the shelf in the app before retrying, because a retry can count as another import |
| `WeRead import limit reached: …` | WeRead's own message follows. Without a paid membership, remove imported books from the shelf; if the message names a period, wait |
| `WeRead is limiting requests` | Wait a while before importing again |
| `WeRead could not read the file as a book` | The upload finished but WeRead could not parse it. Check that the file opens in an ebook reader, or convert it to EPUB |
| `file is too large for WeRead` | The message names the limit for the format. Convert a large TXT file to EPUB, or compress or split the book |
| `WeRead does not accept this file type` | Convert the file to EPUB first |
| `WeRead /cos/... failed (HTTP 200, errCode …)` | WeRead rejected the request; the message shows WeRead's reason |
| Book imported but shows as images or loses text | The file may be an image-only EPUB, or have very large chapter files that WeRead displays poorly; try another edition or convert it again |

## For agents

Agents use the non-interactive forms: `zlib weread import <file> --json` and
`zlib download "id:hash" --send-to-weread --json`. The login needs a phone, so an
agent must ask the user to run `zlib weread login` in their own terminal. The MCP
server exposes `import_to_weread` and the `send_to_weread` field of
`download_book`; see [MCP server](mcp.md).
