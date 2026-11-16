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
| `-s`, `--series`, `-i`, `--index` | Series and position (`2`, `2.5`) |
| `--isbn`, `--identifier scheme:value` | Identifiers; `--identifier asin:` removes one |
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

## Look a book up

```sh
zlib meta book.epub --fetch
zlib meta book.epub --fetch --json
zlib meta book.epub --fetch -t "Dune" -a "Frank Herbert"
zlib meta book.epub --fetch --isbn 9780441013593
```

`--fetch` asks Open Library and Google Books about the book (and Douban or
WeRead, once you enable them: [Douban](#douban), [WeRead](#weread)) and lists
the best matches. It only reads: it never changes the file, and it cannot be
combined with the field options. `-t`, `-a` and `--isbn` set what to search
for instead of what the file says. To use a match, copy the values into the
field options above; the terminal interface applies a match for you.

What it does:

- Sources are asked in parallel. One failing, for example when Google
  Books refuses a request for quota reasons, is reported on stderr and the
  other results are still listed.
- An ISBN gives the exact edition. Without one, the title and authors are
  searched, and results are ranked by how well they agree. A result found
  from a title alone is a guess about the edition: check it before using its
  ISBN or date.
- A result is marked `ISBN match`, or `ISBN match, title differs` when the
  ISBN belongs to a book with another title (files often carry a wrong
  ISBN), or `title match` with a percentage.
- A volume number is part of the title: a title ending in the Roman numeral
  glyph `Ⅱ` agrees with the same title ending in `2`, and neither is mistaken
  for the first volume. Dates written with the Chinese year and month
  characters are read.

### Where the ISBN comes from

A lookup searches, in this order of trust: the ISBN in the file's metadata; an
ISBN recorded by a [download](usage.md#download) under the identifier
`zlib-isbn`; and, when there is none, an ISBN printed in the book itself.

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

### Douban

Douban Books covers Chinese editions that Open Library and Google Books lack,
and each Douban entry is one edition. It is off by default, because it sends
the title to a third party. Turn it on with a source list:

```sh
ZLIB_METADATA_SOURCES=douban,openlibrary,google zlib meta book.epub --fetch
```

Add the line to `.env` to keep it. The names are `openlibrary`, `google`,
`douban` and `weread`; an unknown name is reported as a failing source. An ISBN is resolved
through Douban's ISBN address and a title through its mobile search, then up to
three entries are read. Editions of one title stay separate results, so you
choose the edition. Requests carry no cookie or login, but the mobile
endpoints need a `Referer` of `m.douban.com`, which is sent. Douban throttles:
a refusal is reported like any other source failing. Covers and tags are not
taken from Douban.

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

Keep both lines in `.env`; the key is never printed, logged or put in a URL.
The key belongs to your WeRead account, so each title a lookup sends reaches
Tencent under your identity. If `weread` is listed without a key, the lookup
reports that source as failing and sends nothing.

This key is separate from the [WeRead login](weread.md) that imports books,
and the source does not use that login. Lookups use `ZLIB_WEREAD_PROXY` when it
is set, like the importer, and otherwise the system proxy; `ZLIB_PROXY` does
not apply.

The gateway cannot search by ISBN, so a lookup needs a title: it searches the
title, reads up to three close matches, and compares the ISBN each entry
carries, which is often empty, with the wanted one. A result without an ISBN
is a `title match` whose edition fields start unselected, and two WeRead
entries of one title stay separate results. Covers are not taken from WeRead.
A gateway notice asking clients to upgrade is reported as a failing source
with fixed wording; its text is never shown or followed.

On a terminal, plain `--fetch` opens an interactive page (also available as
`/meta` in the [terminal interface](tui.md)): pick a match, review each field
as `old → new`, and write the ones you choose. What starts selected depends on
how sure the match is, and nothing already in the file is replaced unless you
select it:

| Match | Starts selected |
| --- | --- |
| `ISBN match` | Every field the file leaves empty |
| `title match` | Empty fields that describe the book (authors, description, tags, cover). The publisher, date, language and every identifier, which differ between editions, are shown tagged `check edition` and start unselected |
| `ISBN match, title differs` | Nothing: the rows are tagged `may be another book` |

A guessed ISBN would later be read as an exact ISBN match, so keep one only
after checking the edition.

Formats that cannot store a field do not offer it. Redirected output, `--json`
and `-t`, `-a` or `--isbn` print the list instead.

A match that has a cover adds a `Cover` row. Selecting it downloads the image
when you apply: if the download fails, nothing is written and the page says
why. A cover is only fetched from Open Library, the Internet Archive and Google
Books, must be a JPEG, PNG, GIF or WebP image of at least 50 by 50 pixels, and
is capped at 10 MiB. A MOBI or AZW3 file can only have an existing cover
replaced, so the row is not offered for one without a cover.

After a download in the terminal interface, choose **Fix metadata** on the
saved-file page. The lookup also searches the ISBN from the Z-Library record,
and uses its title and authors when the file has none. Leaving the page
returns to the saved file.

With `--json`, the document has the `query`, the ranked `candidates` (with
`match`, `score`, `sources`, `edition_level` and the fields) and any
per-source `errors`.

Exit codes, for redirected output and `--json`: `1` when nothing matched, `2`
when the file has nothing to search with or `--isbn` is not a valid ISBN, `4`
when every source was unreachable. On a terminal the interactive page reports
these cases itself: leaving it with Esc exits `0`, and a file with nothing to
search with exits `1`.

Google Books answers from a quota shared with other users unless you set an
API key (`zlib config metadata google-key`, or `ZLIB_GOOGLE_BOOKS_KEY`; see
[Configuration](configuration.md)). Without one it often reports `rate limited`
and the results come from Open Library. A rejected key is reported as such.

Lookups and cover downloads use `ZLIB_PROXY` when it is set, otherwise the
system proxy; the Z-Library session is never sent. The one exception is the
`weread` source, which follows `ZLIB_WEREAD_PROXY` like the WeRead importer.
See
[Metadata lookup research](metadata-research.md).

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
