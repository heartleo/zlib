# Metadata lookup research

[README](../README.md) · [E-book metadata](metadata.md)

How `zlib meta --fetch` finds, matches and merges authoritative metadata, and
how that relates to the design of Calibre's metadata sources. Calibre is
GPL-3.0-licensed; zlib's implementation is written from the behaviour
described here and shares no code with it.

Studied revision: `kovidgoyal/calibre` at
`eed88be5ac9fd51c9cc8bed6b77887f33281866d`, files under
`src/calibre/ebooks/metadata/sources/` and `src/calibre/db/cache.py`.

## How Calibre does it

| Stage | Calibre behaviour |
| --- | --- |
| Sources | Each source is a plugin with `identify(title, authors, identifiers)`. Sources are Google Books, Amazon, Edelweiss and Open Library (covers only). |
| Query | An ISBN wins over title and authors. Title and author tokens are cleaned (edition notes in brackets removed, short tokens and joiners dropped) and sent as an AND query. |
| Fan-out | One thread per source. After the first result arrives, the rest get a fixed grace period (default 30 s) and are then aborted. |
| Per-source ranking | Results from one source are sorted by: shares an identifier with the query, has a cover, has every field the source provides, language matches the UI, exact title match, longer description, source's own relevance. |
| Pooling | Results are pooled by ISBN (the old xISBN service for related ISBNs has been decommissioned), then by identical title and authors. A pool keeps one result per source. |
| Merge | Per field: shortest title, publisher and language; longest author list, series and description; fewest tags; mean rating; earliest publication date; union of identifiers. |
| Post-processing | Unicode NFC normalisation, optional tag, author, publisher and series mapping rules, a cap on tags. |
| Applying | Fetched metadata is merged into the library record. Existing values win: title, authors, publisher, series and date are filled only when empty, tags and comments are appended, and existing identifiers override incoming ones. |
| Source updates | Source plugins are downloaded and executed as code. |

## What zlib adopted, changed and left out

Adopted: an ISBN is the strongest key; sources run in parallel and a slow one
does not hold up the rest; a source failing never fails the lookup; results
from several sources for the same book are merged; and applying metadata
fills what is empty before replacing what exists.

Changed:

- **ISBNs are normalised** (checksum validated, ISBN-10 converted to
  ISBN-13) before pooling, so one book has one key whichever form a source
  uses. Calibre pools on the raw string.
- **Matching works on runes, not words.** Calibre splits titles and names on
  spaces and drops short tokens, which loses Chinese, Japanese and Korean
  text. zlib scores rune bigrams (Sørensen–Dice), ignores a subtitle on either
  side, reorders "Last, First" names and drops a bracketed country tag in
  front of a name and a trailing "author" or "translator" word.
- **Different ISBNs are different editions** and are never merged by title.
  Work-level results (an Open Library title search) carry no ISBN, publisher
  or edition date, because they do not say which edition a file is.
- **An ISBN match whose title disagrees is flagged** (score 0.5, "title
  differs") rather than reported as a perfect match: files often carry the
  ISBN of another book.
- **Tags are a capped union**, most-mentioned first, with catalogue
  housekeeping and subject headings dropped, instead of the smallest set.
- **Replacing a value is always an explicit choice.** The default plan only
  fills empty fields, and only as far as the match is certain: after a
  title-only match, edition-specific fields (publisher, date, language,
  identifiers) are offered but not selected, and after an ISBN match whose
  title disagrees nothing is selected. Writing a guessed ISBN would make the
  next lookup treat it as exact. A `uuid` is never taken from a catalogue.

Covers are fetched only when the user selects them. Calibre downloads covers
from every source and keeps the best by a per-source priority and then by
resolution. zlib keeps one URL per book, preferring Open Library's large cover
over Google's thumbnail, and checks the download itself: https only, an
allow-listed host re-checked on every redirect (Open Library covers can
redirect to Internet Archive hosts), at most 10 MiB, a real JPEG, PNG, GIF or
WebP image, and at least 50 pixels on each side.

For a book downloaded through zlib, the Z-Library record's own ISBN is added
to the lookup. The record is validated like any other ISBN, and its title and
authors are used only to fill in what the file lacks. This is the main help
for books in other languages, where a title search finds little.

Left out: Amazon and Edelweiss (HTML scraping that needs a headless browser,
which zlib does not do), rating, series (neither source provides it), user
mapping rules, and source updates (downloading code to execute is not
something to copy).

## Sources and evidence

All requests go through a client with no cookie jar and no credentials, a
fixed host list that is re-checked on every redirect, a 4 MiB body limit and a
generic User-Agent. Errors never include the request URL, which carries the
title.

| Source | Request | Used for |
| --- | --- | --- |
| Open Library | `GET /api/books?bibkeys=ISBN:…&format=json&jscmd=data` | Edition by ISBN: title, subtitle, authors, publisher, date, subjects, identifiers, cover |
| Open Library | `GET /search.json?title=…&author=…&fields=…` | Works by title and author: title, authors, first publication year, subjects, cover |
| Google Books | `GET /books/v1/volumes?q=isbn:…` or `intitle:"…" inauthor:"…"` | Edition: title, authors, publisher, date, description, categories, language, ISBN, thumbnail |
| Open Library covers, Google Books thumbnails | `GET` of the cover URL a result names | The cover image, on request |

Live measurements with a well-known public ISBN, status codes and key names
only:

- Open Library `/api/books` and `/search.json`: 200, JSON. Edition keys:
  `title`, `authors`, `publishers`, `publish_date`, `subjects`,
  `identifiers`, `cover`. Search document keys with `fields`: `key`,
  `title`, `author_name`, `first_publish_year`, `subject`, `language`,
  `publisher`, `cover_i`, `edition_key`.
- In a work-level search `publisher` lists the publishers of every edition,
  so it is not used.
- Open Library `/isbn/{isbn}.json`: 200 after one same-host redirect.
- Open Library covers with `?default=false`: 200 with a JPEG.
- Google Books v1 without a key: 429 `RESOURCE_EXHAUSTED` with reason
  `rateLimitExceeded` on repeated requests from one network. The quota is
  shared, so a lookup treats it as that source being unavailable and still
  returns the other sources' results.

## Douban and Chinese matching

Studied revision: `Marvae/douban-cli` (MIT), the `search`, `book` and
`resolve` modules. zlib's code is written in Go from the behaviour below and
shares none of it.

| Behaviour | What zlib does |
| --- | --- |
| An ISBN is resolved through the `/isbn/<isbn>/` redirect, which is not throttled like search | Reads the subject ID from the `Location` header and downloads no page |
| Title search through the mobile search API, details through the mobile detail API; both need a `Referer` on `m.douban.com` | Same endpoints, no cookie, no login, a generic User-Agent, `Referer` only |
| An exact title wins over a subtitle match, and several editions of one title are never collapsed to the first hit | Results are ranked by title similarity and each Douban entry stays its own candidate; two different Douban entries are never pooled, like two ISBNs |
| A sequel whose title ends in a Roman numeral glyph such as `Ⅱ` must not collapse into the first volume | `foldText` turns Roman numeral glyphs into digits, so the volume is part of the key (before, the glyph was dropped and the sequel scored as a perfect match) |

Not taken from Douban: covers (another host, with hotlink rules), ratings and
series. Tags are the first five readers' labels that do not repeat the title or
an author.

The endpoints are the ones `douban-cli` uses and are not an API Douban
documents. The field names come from that project's source and have not been
measured against the live service. The detail response's `isbn` field in
particular is unverified: it is parsed when present and validated, and nothing
depends on it, because an ISBN lookup already knows the ISBN it asked for.

## ISBNs recorded by a download

The Z-Library record's ISBN is the cheapest way to fix a Chinese book's
metadata, since a title search finds little. It is not trustworthy enough to
write as the file's ISBN unchecked: a written ISBN is read as exact by every
later lookup, and records and files do carry the ISBN of another book. So:

- It is written as `isbn` only when the file has no ISBN and the record's
  title is at least 0.8 similar to the file's (the matcher's own threshold is
  0.5, since a write is permanent).
- Otherwise it is written as `zlib-isbn`. A lookup searches it, and the
  matcher's existing rule does the verifying: a hit whose title agrees is a
  certain ISBN match and offers to fill `isbn`; a hit whose title disagrees is
  scored 0.5 and flagged `may be another book`.
- An ISBN in the file is never replaced, and a format that cannot store
  `zlib-isbn` (MOBI keeps ISBN and ASIN only) simply records nothing.

For books downloaded earlier, the book's own text is read: the front 16 and
last 4 content documents of an EPUB and the body of a DOCX, for the text after
an `ISBN` label or a hyphenated ISBN-13, kept only when the check digit is
right. MOBI, AZW3 and PDF text is compressed, and a scan of it is not done.

## Limits

- Coverage of Chinese-language books is thin in both default catalogues;
  Douban is the opt-in answer.
- Simplified and Traditional Chinese are not folded together: a Traditional
  title in a file does not match a Simplified one by text alone, only by ISBN.
- A title search is only as good as the title in the file. ISBNs give exact
  editions.
- Without a Google Books API key, Google answers from a shared quota and
  often refuses. An optional key (`ZLIB_GOOGLE_BOOKS_KEY`, set with
  `zlib config metadata`) gives a lookup its own quota. It is sent in the
  `x-goog-api-key` header, which Google accepts in place of the `key` query
  parameter (a deliberately invalid key is refused with 400 `API_KEY_INVALID`
  either way), so it never appears in a URL, a log or an error. A rejected key
  and a key over its quota are reported as such, and a malformed value is not
  sent at all.
