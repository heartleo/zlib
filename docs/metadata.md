# E-book metadata

`zlib meta` shows and edits the metadata stored inside a local e-book file:
title, authors, series, identifiers, cover and so on. It works offline and
needs no login. The format is detected from the file contents, not the
extension.

```sh
zlib meta book.epub
zlib meta book.epub --json
```

## Change fields

Any field option rewrites the file in place and then prints the result.

```sh
zlib meta book.epub -t "New Title" -a "Jane Doe & John Roe"
zlib meta book.epub --series "Saga" --index 2 --tags "fiction, space"
zlib meta book.epub --isbn 9780000000002 --identifier asin:B000000000
zlib meta book.epub --cover cover.jpg
```

| Option | Field |
| --- | --- |
| `-t`, `--title` | Title |
| `--title-sort` | Title used for sorting |
| `-a`, `--authors` | Authors, separated by `&` |
| `--author-sort` | Author name used for sorting |
| `-p`, `--publisher` | Publisher |
| `-l`, `--language` | Languages, comma-separated (`en,fr`) |
| `--tags` | Tags, comma-separated |
| `-s`, `--series`, `-i`, `--index` | Series and position (`2`, `2.5`); `--index` needs a series, from the file or from `--series` |
| `--isbn`, `--identifier scheme:value` | Identifiers; `--identifier asin:` removes one. An ISBN must be valid and is stored as an ISBN-13 |
| `-d`, `--date` | Publication date (`2024-03-04`) |
| `-c`, `--comments` | Description |
| `-r`, `--rating` | Rating, 0 to 5 stars |
| `--cover FILE` | Cover image (JPEG, PNG, GIF or WebP) |

An empty value clears a field: `--series ""`. Fields you do not name are left
exactly as they are, with one exception: a new title or new authors clear the
old title or author sort value unless `--title-sort` or `--author-sort` is
given too.

The new file is written next to the original and renamed over it only when
complete, so an error leaves the book untouched. Only the parts that hold
metadata change; chapters, images and fonts are copied byte for byte.

## Export and import

```sh
zlib meta book.epub --get-cover cover.jpg
zlib meta book.epub --to-opf book.opf
zlib meta other.epub --from-opf book.opf
```

`--to-opf` writes a standalone OPF 2.0 package document, the sidecar format
e-book managers exchange. `--from-opf` applies every field present in an OPF
file; options given alongside it take precedence.

## Rename a file

```sh
zlib meta book.epub --rename
zlib meta book.epub -t "Dune" -a "Frank Herbert" --rename
zlib meta book.epub --rename-to "Frank Herbert - Dune"
```

`--rename` names the file from the title and authors a lookup would search for:
the file name first, then the file, as [described below](#what-the-search-is-built-from).
The order of the name is the file name format ([below](#file-name-format)).
When the same command sets the title, the authors or an OPF file, the file's own
metadata is what you just chose and is used instead. `--rename-to` takes the name as you
type it; the extension is kept. Both stay in the file's directory, keep the
extension, never replace another file (a clash is an error and nothing moves)
and print the new path with `--json` (`file`, and `renamed_from`). `--fetch`
only reads, so it cannot be combined with either.

A name is `Title - Authors.ext` by default, or `Title.ext` when no author is
known:

- Up to three authors, separated by commas. `Last, First` is written
  `First Last`, since a comma would split it into two people.
- A subtitle is written after two spaces, not a colon, which Windows does not
  accept; zlib reads two spaces back as a colon, so the file is found again.
- No path separators or characters Windows rejects (`/ \ : " < > | ? *`), no
  control or bidirectional marks, no leading or trailing dot, and no reserved
  Windows device name. The name is at most 255 bytes, cut in the title.
- A title that is a placeholder (`Unknown`) gives no name, and a file with no
  title anywhere is an error. Embedded titles often carry a marketing line,
  which is why the file name's title is preferred; the terminal interface
  offers the file's own title as a separate choice.

- A title that ends in a `(...)` group is written with square brackets,
  because a Z-Library name reads that group as the authors.
- The extension is kept, in lower case, when it is an e-book's. A file with no
  extension, or a dot that is part of its name, keeps its name without one.

A name is read back in the same order (see
[what the search is built from](#what-the-search-is-built-from)), so a second
`--rename` of a renamed file changes nothing. The checks that can be made before
anything is written are: using both options, an empty name, a name that is
taken, a file with no title to name it by. They run before the other options of
the same command write the file, so a refused rename leaves it untouched.

### File name format

The order of the title and the authors in a name is one of three formats:

| Format | Name | Good for |
| --- | --- | --- |
| `title-author` (default) | `Dune - Frank Herbert.epub` | a folder that sorts by title |
| `author-title` | `Frank Herbert - Dune.epub` | a folder that sorts by author |
| `title-paren` | `Dune (Frank Herbert).epub` | names as Z-Library gives them |

Set it with `zlib config metadata filename` (or `ZLIB_FILENAME_FORMAT`; empty
restores `title-author`). In Settings it is a list of the three formats with a
`Custom pattern…` row, which opens a text box that shows what the pattern does to
a sample book as you type.

A custom pattern writes `{title}` once and `{authors}` at most once, with any
other text kept as written, for example `{title} [{authors}]` or
`{authors}, {title}`. The text around the fields is at most 40 characters and
cannot contain `/ \ : " < > | ? *`. A pattern that names the authors gives the
bare title when none is known, and a pattern with no `{authors}` never names
them. The rename page offers a configured pattern as a fourth format, key `4`. It applies to `--rename`, to the `suggested_file_name`
of `--fetch --json` and to the rename page, which can also switch format for one
rename and save the choice as the default.

A dashed name has no marker that says which side is the title, so it is read
as the file says: the side that holds the file's (or the download's) authors is
the authors, and the side that holds its title is the title. When the file gives
nothing to compare, the configured format decides; `title-paren` has no such
doubt, because the authors are in brackets.

A plain `--fetch` prints what the file already holds, under `In the file`,
before the matches, and before any catalogue is asked; `--fetch --json` carries
the same as `current`.

`--fetch --json` adds a `suggested_file_name` to each candidate, built from the
candidate's title and authors with the extension of the file looked up. In the
terminal interface, `n` on the prepare, results and fields pages renames the
file: see [the page](tui.md).

## Look a book up

```sh
zlib meta book.epub --fetch
zlib meta book.epub --fetch --json
zlib meta book.epub --fetch -t "Dune" -a "Frank Herbert"
zlib meta book.epub --fetch --isbn 9780441013593
zlib meta book.epub --fetch --source douban,weread
```

`--fetch` asks the catalogues for the book's language ([see below](#default-sources))
and lists the best matches. It only reads: it never changes the file, and it cannot be
combined with the field options. `-t`, `-a` and `--isbn` set what to search
for instead of what the file says. To use a match, copy the values into the
field options above; the terminal interface applies a match for you.

`--source` picks the catalogues for that run: `openlibrary`, `google`,
`goodreads`, `douban` and `weread`, comma-separated or repeated. It needs `--fetch`, it
beats `ZLIB_METADATA_SOURCES`, and an unknown name is a usage error (exit `2`).
It is never saved. Without it a lookup asks the sources in
`ZLIB_METADATA_SOURCES`, or the default sources for the book when that is unset.

#### Default sources

With no list chosen, the sources follow the book's language:

| Book | Sources |
| --- | --- |
| Chinese | Douban |
| Any other | Open Library, and Google Books too when a Google Books API key is set (see below) |

A book is Chinese when its language is Chinese, or when its title, first author
or file name (or, for a download, the Z-Library record) is written in Han
characters: a Chinese book is often tagged with the wrong language. Kana and
hangul rule it out, so a Japanese or Korean book is not Chinese. Goodreads and
WeRead are never defaults. A list in `ZLIB_METADATA_SOURCES` or `--source` replaces the
defaults for every book; the prepare page shows which sources are on and lets
you change them, and Ctrl+D saves the choice as that list.

What it does:

- Sources are asked in parallel. One failing, for example when Google
  Books refuses a request for quota reasons, is reported on stderr and the
  other results are still listed.
- Results are not merged across sources: each source's answer is shown as
  that source says it, in one row or several, and every row names its source.
  The same book from three sources is three adjacent rows. A source that lists
  one book twice (the same ISBN) still has one row for it.
- At most 10 results are listed for each source, best first across all of them.
  `zlib config metadata results` (or `ZLIB_METADATA_RESULTS`) sets the limit, 1
  to 50; empty restores 10. Each source is asked for enough hits to fill it.
  Douban and WeRead read one page per hit, at most 10, so a high limit costs
  them more requests.
- An ISBN gives the exact edition. Without one, the title and authors are
  searched, and results are ranked by how well they agree. A result found
  from a title alone is a guess about the edition: check it before using its
  ISBN or date.
- A result is marked `ISBN match`, or `ISBN match, not confirmed` when
  nothing vouches for the ISBN: it belongs to a book with another title (files
  often carry a wrong ISBN), or it is only a hint and the search has no title
  that was not read from the same source. Otherwise it is `title match` with a
  percentage.
- A volume number is part of the title: a title ending in the Roman numeral
  glyph `Ⅱ` agrees with the same title ending in `2`, and neither is mistaken
  for the first volume. Dates written with the Chinese year and month
  characters are read.

### What the search is built from

The search is built from the file name first and then the file's own
metadata, the same way in the terminal page and in `--fetch`:

- **Title.** A Z-Library name such as `Title (Author).epub` is usually cleaner
  than the embedded title, which may carry a marketing line or a placeholder
  (`Unknown`, a UUID, a long number such as an ISBN or a date; a short one such as `1984` is a title), so the file name's title is used and the
  embedded one is kept as the other choice (`t` on the page). A title read
  from a name or a download record is a guess: it cannot by itself confirm an
  ISBN that is only a hint.
- **Authors.** For a Chinese title, the side that writes the names in Chinese
  when only one does. Otherwise the file's authors when they name the same
  person as the file name (a name often adds the translator), and the file
  name's authors when they do not.
- **Edition hints.** The file's publisher and year rank editions of one title a
  little; they never exclude one.
- **A dashed name.** `Title - Authors` or `Authors - Title`, the names
  [`--rename`](#rename-a-file) writes, is read that way when the file's or the download's authors are the
  words before the dash, or when the file has no title or authors of its own to
  contradict it. When only the title agrees, the file's authors stand. A
  Z-Library name that ends in a `(...)` group keeps that group as its authors,
  and is read as a dashed name only when the file's authors are the words
  before the dash. Otherwise the whole name is the title, since a title can
  have a dash in it.

`--json` adds an `extraction` object with what the file name and the file said
and where the title, authors and each ISBN came from.

### Where the ISBN comes from

A lookup searches, in this order of trust: the ISBN in the file's metadata; an
ISBN recorded by a [download](usage.md#download) under the identifier
`zlib-isbn`; an ISBN printed in the book itself; the Z-Library record of a
download just made; and an ISBN written in the file name. Only the first is
the file's own and confirmed. The others are hints: a hint is confirmed only
when a result carries it and its title agrees with the file's own title or one
you edited, never with a title that was itself read from the file name or the
download record.

- **Recorded by a download.** `zlib download` writes the ISBN from Z-Library's
  record into the saved file. It becomes the file's `isbn` only when the file
  has none, the record's title agrees closely with the file's title, and the
  record names one edition: a record that lists the ISBNs of several editions
  cannot say which one the file is. Otherwise it is stored as `zlib-isbn`, a
  hint that is searched but never
  taken as the book's own ISBN. A later lookup that finds the same ISBN on a
  book with the same title confirms it, and the page then offers to fill the
  empty `isbn`. Use `--no-embed` to skip this.
- **Printed in the book.** For a book without either, the lookup reads the
  front and last pages of an EPUB or DOCX for an ISBN after the word `ISBN`
  (or a hyphenated ISBN-13). Only a number with a correct check digit counts,
  and the result is checked against the title like any other ISBN. MOBI, AZW3
  and PDF text is compressed and is not read. This is how books downloaded
  before the download recorded an ISBN get an exact match.

### Goodreads

Goodreads knows most books, in many languages, with their series, categories and
a cover. It is off by default, because it sends the title, authors or ISBN to a
third party. List it to ask it, alone or with other sources:

```sh
zlib meta book.epub --fetch --source openlibrary,goodreads
ZLIB_METADATA_SOURCES=openlibrary,goodreads zlib meta book.epub --fetch
```

zlib reads three Goodreads addresses, with no cookie or login. A title, or a
title and the first author, is searched through Goodreads' suggestion list,
which returns a few books as JSON. For the first three of them the book's own
page is read, which holds the whole record of that edition: the authors, the
publisher, the publication date, the language, the ISBN, the categories (as
tags), the series, the full description and the cover. An ISBN is resolved
through Goodreads' ISBN address, which redirects to the book, and the page of
that book is read. So a row from a page is an edition, with its own ISBN, and a
result found by title that came from a page carries the edition's ISBN too. A
Goodreads edition is one book ID, which is also the `goodreads` identifier that
is written. The page names the translator as well as the author: only the
author is taken. A Chinese edition may list an author's name in Latin script and,
right after it, in Han script; that pair is one author and the Han name is kept,
and every other author is kept as listed. The publication date is the day
Goodreads holds, whatever precision the edition really has.

Goodreads sometimes answers a page with a bot check instead of the page, and
zlib does not answer it. The source then uses the suggestion for that book, with
only a title, authors, a series and a cover, and reads no more pages in that
lookup, so a lookup is never lost to it; a request it answers with 502, 503 or
504 is tried once more, and a rate limit also ends the reading. The suggestion's
blurb is cut short, and a cut-short blurb is not offered as a description, so it
never replaces a whole one. The suggestion writes a book of a series as
`Title (Series, #2)`, and the series and the position are read from that, where
a box set's range such as `#1-3` is not a position. A series belongs to the work, so a match found by title may
fill an empty series, and one the file already has is never replaced unless you
choose it. The cover is the full-size image from Goodreads' image servers
(`m.media-amazon.com` for a page, `i.gr-assets.com` for a suggestion).

Goodreads' pages are asked for with a desktop browser's `User-Agent` and not
zlib's own, which is a decision of the project's owner and applies to this source
only. It is not configurable from the command line, the environment or the
settings.

### Douban

Douban Books covers Chinese editions that Open Library and Google Books lack,
and each Douban entry is one edition. It is the default for a Chinese book and
is not asked about any other, because it sends the title to a third party. To
ask it for every book, or together with other sources, use a source list:

```sh
ZLIB_METADATA_SOURCES=douban,openlibrary,google zlib meta book.epub --fetch
```

Add the line to `.env` to keep it, or press Ctrl+D on the prepare page of the
terminal interface, which writes it for you. The names are `openlibrary`, `google`,
`goodreads`, `douban` and `weread`; with `--fetch` an unknown name is reported as a failing
source, and the prepare page leaves it out. An ISBN is resolved
through Douban's ISBN address and a title through its mobile search, then up
to ten entries are read (as many as the result limit, at most 10). Editions of one title stay separate results, so you
choose the edition. Requests carry no cookie or login, but the mobile
endpoints need a `Referer` of `m.douban.com`, which is sent. Douban throttles:
a refusal is reported like any other source failing. The cover is taken from
Douban (its image servers need a Douban `Referer`, which is sent to them
only); tags are not.

A result found by title carries the Douban entry's ID but no ISBN, because
Douban's detail response has none, so it is a `title match` whose edition
fields start unselected. A result found by an ISBN carries that ISBN.

### WeRead

WeRead has Chinese e-book entries with the publisher, publication date,
author and category as WeRead lists them. It is off by default, and it needs
an API key: the one Tencent issues for its agent gateway (see
[Tencent/WeChatReading](https://github.com/Tencent/WeChatReading)). Set it and
list the source:

```sh
ZLIB_WEREAD_API_KEY=<your key>
ZLIB_METADATA_SOURCES=openlibrary,google,weread
```

Or set the key in Settings: `zlib config metadata weread-key` (or the Metadata
page of `zlib config`) takes it in a hidden box, saves it to the global `.env`
and shows it only as `Configured (hidden)`; Ctrl+R then Enter removes it, and
the sources checklist lets you tick WeRead once a key is set.

Keep both lines in `.env`; the key is never printed, logged or put in a URL.
The key belongs to your WeRead account, so each title a lookup sends reaches
Tencent under your identity. If `weread` is listed without a key, the lookup
reports that source as failing and sends nothing.

This key is separate from the [WeRead login](weread.md) that imports books,
and the source does not use that login. Lookups use `ZLIB_WEREAD_PROXY` when it
is set, like the importer, and otherwise the system proxy; `ZLIB_PROXY` does
not apply.

The gateway cannot search by ISBN, so a lookup needs a title: it searches the
title, reads up to ten close matches (as many as the result limit, at most
10), and compares the ISBN each entry
carries, which is often empty, with the wanted one. A result without an ISBN
is a `title match` whose edition fields start unselected, and two WeRead
entries of one title stay separate results. Covers are not taken from WeRead.
A gateway notice asking clients to upgrade is reported as a failing source
with fixed wording; its text is never shown or followed.

On a terminal, `zlib meta` with no file (or with only `--fetch`, optionally
with `--source`) opens a file
picker for the formats below, then the lookup page for the file you choose. With
a file, plain `--fetch` opens that page directly. The page is also available as
`/meta` in the [terminal interface](tui.md): pick a match, review each field
as `old → new`, and write the ones you choose. What starts selected depends on
how sure the match is, and nothing already in the file is replaced unless you
select it:

| Match | Starts selected |
| --- | --- |
| `ISBN match` | Every field the file leaves empty |
| `title match` | Empty fields that describe the book (authors, description, tags, cover). The publisher, date, language and every identifier, which differ between editions, are shown tagged `check edition` and start unselected |
| `ISBN match, not confirmed` | Nothing: the rows are tagged `may be another book` |

A guessed ISBN would later be read as an exact ISBN match, so keep one only
after checking the edition.

The page opens on a prepare step and sends nothing until you press Enter. It
shows the book as the file has it in a detail card at the top (its cover beside
its metadata; everything in full on `i`), then the search built from the file
name and the file and where each part came from, and below them the sources to
ask with `[x]` or `[ ]`. Space
toggles a source, `e` edits the search, `t` uses the other title and Ctrl+D saves
the sources that are on as the default (`ZLIB_METADATA_SOURCES` in the global
`.env`); nothing is saved unless you press it. The page starts from, in this
order, `--source`, a real environment variable, a `.env` in the working
directory, the saved default and then the defaults for the book's language. When a
real environment variable or the working-directory `.env` overrides the saved
default, the page says so after saving. Saving also replaces `--source` for the
rest of that session unless the environment overrides the saved list.
`zlib config metadata sources` (or the metadata page of `zlib config`) opens
the sources as a checklist, like the default search formats: Space ticks a
source, Ctrl+A ticks every source that can run (or clears them), Enter saves
and Esc cancels. A source that needs a key says so and cannot be ticked
without it. Nothing ticked saves nothing, which restores the defaults for the
book's language.

The page lets you steer the lookup and fix what it proposes, before anything
is written:

| Key | Where | What it does |
| --- | --- | --- |
| `e` | Candidates, or no match | Edit the title, authors and ISBNs searched for, then search again. Only the search changes, never the file. A file with no title, author or ISBN opens on this editor |
| `m` | Prepare | Edit the file's own metadata, every field, with no lookup |
| `→` `←` space | Fields | Take the match's value for the focused field, keep the file's, or switch between them |
| `x` `u` | Fields | Clear the focused field; undo every change |
| `a` | Fields | Take the match's value for every field it proposes, or for none |
| `?` | Any page but a search or a write in progress | List every key of the page |
| `n` | Prepare, candidates, no match, fields | Rename the file: choose a name from the search terms, the file's own title or the selected match, or type one. Takes effect at once and never replaces another file |
| `s` | Candidates, or no match | Go back to the prepare page to change the sources: Open Library, Google Books, Goodreads, Douban, WeRead. A source whose key is missing says so |
| `e` | Fields | Type a value for the focused field (title, title sort, authors, author sort, series, publisher, languages, tags, date, ISBN, rating, description, cover image). An invalid value is refused with the reason |

Each candidate row ends with the sources behind it in brackets, and the
details say which ISBN the match has, or `no ISBN listed`.

Formats that cannot store a field do not offer it. Redirected output, `--json`
and `-t`, `-a` or `--isbn` print the list instead.

A match that has a cover adds a `Cover` row. Selecting it downloads the image
when you apply: if the download fails, nothing is written and the page says
why. A cover is only fetched from Open Library, the Internet Archive, Google Books,
Goodreads and Douban, must be a JPEG, PNG, GIF or WebP image of at least 50 by 50 pixels, and
is capped at 10 MiB. A MOBI or AZW3 file can only have an existing cover
replaced, so the row is not offered for one without a cover.

After a download in the terminal interface, choose **Fix metadata** on the
saved-file page. The page opens on the prepare step like any other: the
Z-Library record's ISBN is among the hints, and its title and authors fill in
what neither the file name nor the file gives. Leaving the page returns to the
saved file.

With `--json`, the document has the `query`, the ranked `candidates` (with
`match`, `score`, `sources`, `edition_level` and the fields) and any
per-source `errors`.

Exit codes, for redirected output and `--json`: `1` when nothing matched, `2`
when the file has nothing to search with or `--isbn` is not a valid ISBN, `4`
when every source was unreachable. On a terminal the interactive page reports
these cases itself: leaving it exits `0` (Esc on the results goes back to the
search page first, and Esc there leaves; `q` leaves at once), and a file with nothing to
search with opens the search editor, so you can type a title, author or ISBN
(leaving without searching exits `0`).

Google Books answers from a quota shared with other users unless you set an
API key (`zlib config metadata google-key`, or `ZLIB_GOOGLE_BOOKS_KEY`, or
`GOOGLE_BOOKS_API_KEY` when that is empty; see
[Configuration](configuration.md)). With a key, Google Books is a default source
for books that are not Chinese. Without one it is not asked unless you choose it,
and then it often reports `rate limited` and the results come from Open Library.
A rejected key is reported as such.

Lookups and cover downloads use `ZLIB_PROXY` when it is set, otherwise the
system proxy; the Z-Library session is never sent. The one exception is the
`weread` source, which follows `ZLIB_WEREAD_PROXY` like the WeRead importer.

## Formats

| Format | Fields read and written |
| --- | --- |
| EPUB 2 and 3 | All fields and the cover |
| FB2 | Title, authors, publisher, language, tags (genres), series, ISBN, date, description, cover |
| DOCX | Title, authors, languages, tags (keywords), description |
| MOBI, AZW, AZW3 | Title, authors, publisher, languages, tags, ISBN, ASIN, date, description, cover (replace only) |
| PDF | Title, authors, tags (keywords), description (subject) |

When a format cannot store a requested field, `zlib meta` names it on stderr,
skips it and still writes the rest.

### EPUB notes

- The package document keeps its version. EPUB 3 books get `refines`
  metadata and a `belongs-to-collection` series; EPUB 2 books get `opf:role`,
  `opf:file-as` and `calibre:series` metadata. The series is written in both
  forms in EPUB 3 so older readers find it.
- A write updates the package's `dcterms:modified` to the time of the write, in
  UTC, as EPUB 3.3 asks; EPUB 2 has no such property.
- Identifiers without a registered URN namespace are written `scheme:value`
  (for example `zlib-id:123:abc`, `douban:1003078`), the form Calibre also
  writes, and are read back in either EPUB version.
- The package's unique identifier is never removed; setting a new value for
  its scheme rewrites it in place.
- A new cover replaces the bytes of the existing cover image. A book without
  one gets a new image item marked as the cover.
- The archive keeps the `mimetype` entry first and uncompressed. Files listed
  in `META-INF/encryption.xml` are never rewritten; replacing an encrypted
  cover fails with an error.

### FB2 notes

- Fields live in `<title-info>` and `<publish-info>`; new elements are
  placed where the FictionBook schema expects them.
- Tags are FB2 genre codes such as `sf_fantasy`. FB2 holds one book
  language, so only the first `--language` value is stored.
- Author names are split at the last word: `Jane Q. Doe` becomes first name
  `Jane Q.` and last name `Doe`; a single word becomes a nickname.
- A file in `windows-1251`, `windows-1252`, `koi8-r` or `iso-8859-1` is
  rewritten as UTF-8, with its XML declaration updated to match.

### DOCX notes

- Fields live in the core properties part (`docProps/core.xml`). Authors are
  stored `;`-separated in `dc:creator`, tags comma-separated in
  `cp:keywords`. A document without the part gets one.

### MOBI and AZW3 notes

- Fields are EXTH records in the header record. Joint MOBI/KF8 files have
  both headers updated. Unknown EXTH records are kept.
- The header record keeps its size when the new metadata fits in its
  padding, so no other record moves; otherwise later records shift and the
  record table is rewritten.
- `--cover` replaces the existing cover image. A file without one cannot get
  a cover added. DRM-protected files can be read but not written.

### PDF notes

- Fields are the document information dictionary (`Title`, `Author`,
  `Subject`, `Keywords`); other entries such as `Producer` are kept.
- Writes append an incremental update. The original bytes stay in place,
  followed by a new information dictionary and a cross-reference section of
  the same kind the file already uses (table or stream).
- Encrypted PDFs are refused. An XMP metadata stream, if the file has one, is
  not updated, and some readers prefer it over the information dictionary.
- Only `FlateDecode` cross-reference and object streams are read; damaged
  files are not reconstructed.

## Safety

Metadata is untrusted text. Control and bidirectional formatting characters
are removed when a file is read, so a crafted title cannot reach the terminal
as an escape sequence.

Exit codes follow [Usage](usage.md#exit-codes): `2` for an invalid option
value, `1` for an unreadable or unsupported file.
