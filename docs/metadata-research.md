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
  fills empty fields. A `uuid` is never taken from a catalogue.

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

## Limits

- Coverage of Chinese-language books is thin in both catalogues.
- A title search is only as good as the title in the file. ISBNs give exact
  editions.
- There is no API key setting, so Google results depend on the shared quota.
