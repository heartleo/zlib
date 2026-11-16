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

`--fetch` asks Open Library and Google Books about the book and lists the
best matches. It only reads: it never changes the file, and it cannot be
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
system proxy; the Z-Library session is never sent. Unlike WeRead, which has its
own proxy setting, they share the Z-Library one. See
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
