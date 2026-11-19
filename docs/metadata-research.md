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
WebP image, and at least 50 pixels on each side. Douban's image hosts
(`img<N>.doubanio.com`) are on the list too; they answer a request without a
Douban `Referer` with HTTP 418, so one is sent to those hosts only. So is
Goodreads' image host (`i.gr-assets.com`), which needs no `Referer`.

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

The cover is taken from Douban: the detail response's `cover_url` names the
large image on an `img<N>.doubanio.com` host, which needs a `Referer` (see the
measurements below). Not taken from Douban: ratings, series and tags. The detail response's `tags` list was empty in the
measurement below, and `douban-cli` reads tags from a separate endpoint.

The endpoints are the ones `douban-cli` uses and are not an API Douban
documents. Live measurements, status codes and key names only, made with
`curl` and a `Referer` on the matching Douban host:

| Request | Result |
| --- | --- |
| `GET book.douban.com/isbn/<isbn>/` | 301 with an absolute `Location` of `https://book.douban.com/subject/<id>/`. The response sets a `bid` cookie, which is ignored |
| `GET m.douban.com/rexxar/api/v2/search/book?q=…&start=0&count=3` | 200, JSON. Top level: `banned` (an empty string), `count`, `items`. Item: `layout`, `target`, `target_id`, `target_type` (`book`), `type_name`. Target: `id` (a string), `title`, `card_subtitle`, `abstract`, `cover_url`, `rating`, `uri`. A query with raw non-ASCII bytes in the URL was refused with 400 `Bad Request`; the percent-encoded query is accepted |
| `GET m.douban.com/rexxar/api/v2/book/<id>` | 200, JSON. Used: `title` (string), `author`, `press`, `pubdate` (lists of strings, a date such as `2008-1`) and `intro` (string). Present and unused: `pages`, `translator`, `subtitle`, `book_subtitle`, `catalog`, `book_series` and others. Also used: `cover_url`, an https URL to the large cover (`pic` has `large` and `normal` variants of the same image). Absent: `isbn`, `isbn10`, `isbn13`, `language`, `publisher`, `original_title`, `summary` |
| `GET img<N>.doubanio.com/view/subject/l/public/<name>.jpg` | 418 with a 14-byte body and no `Referer`; 200 `image/jpeg` with a `Referer` of `m.douban.com` or `book.douban.com` (a 350x500 JPEG for the entry measured) |

Consequences: a title search yields candidates that name their edition by the
Douban subject ID only, with no ISBN, which is why two of them are never
pooled and why one never inherits another source's ISBN match. Only an ISBN
lookup returns an ISBN, the one it asked for. Reading an ISBN from a
subject's HTML page would need page scraping, which zlib does not do.

Not measured: whether Douban accepts zlib's own `User-Agent` (the
measurements used the `curl` default), and its throttling limits.

## Goodreads

Studied revision: `bookorbit/bookorbit` at
`c1dd6b4f1947ff50843a155e12e70f1e7d15f0c7`, the Goodreads provider of its
metadata fetch (the provider, mapper and types files). zlib's source is written
in Go from the behaviour below and the measurements, and shares no code with it.

| Behaviour | What zlib does |
| --- | --- |
| Search the suggestion endpoint first, because the HTML pages are gated by a bot challenge | Uses only the suggestion endpoint and the ISBN redirect; it never reads a book page |
| A bot challenge is a 202 with a script, not a block status; a plain request cannot pass it, and carrying on only keeps it up | The 202 is reported as the source failing, once, and the lookup carries on with the other sources. zlib does not solve the challenge or imitate a browser to avoid it: the browser-like request headers BookOrbit sends are not sent |
| The book page's embedded state is the only source of a full description, a publisher, a date, a language and an ISBN | Not used, since the page is not read. The suggestion blurb is kept only when it is not marked as cut short |
| The suggestion list for an ISBN is searched and its hit is mapped | The ISBN is first resolved through the redirect, and only the hit whose book ID is the redirect's is taken, so another book in the list cannot be mistaken for the ISBN's |
| A short pause and a few retries for the 503 that Goodreads answers with under load | One retry after a short wait for 502, 503 or 504, ended by the lookup's own timeout. 500 is never retried |
| Results are scored by title, author, rating count and a penalty for summaries and study guides | The lookup's own ranking and title similarity decide. The live endpoint returned a study-guide hit for a one-word title, and its title did not reach the similarity threshold, so no separate filter was added |

zlib's requests carry its own `User-Agent`, no cookie and no login. Live
measurements, status codes and key names only:

| Request | Result |
| --- | --- |
| `GET /book/auto_complete?format=json&q=<isbn>` | 200, `application/json`, a list of one hit |
| `GET /book/auto_complete?format=json&q=<title and author>` | 200, a list of up to five hits |
| A hit | `bookId` (a string), `workId`, `bookUrl` (a path ending `/book/show/<id>` and a slug, after `-` or `.`), `title` (with a ` (Series, #n)` suffix when it is in one), `bookTitleBare`, `author` (an object: `id`, `name`, `isGoodreadsAuthor`, `profileUrl`, `worksListUrl`), `numPages` (a number), `avgRating` (a string), `ratingsCount` (a number), `imageUrl`, `description` (an object: `html`, `truncated`, `fullContentUrl`), `qid`, `rank`, `from_search`, `from_srp`, `kcrPreviewUrl` |
| `description.truncated` | `true` in every hit measured |
| `GET /book/isbn/<isbn>` | 301 with an absolute `Location` of `/book/show/<id>-<slug>` on the same host. The ID is the one the suggestion list gives for the same ISBN |
| `GET /book/show/<id>` | 202, `text/html`, a few kilobytes: a script and the markers `awsWafCookieDomainList` and `challenge-container`, and none of the page's data. A browser-like `User-Agent` was not measured and is not planned |
| `GET` of a hit's `imageUrl` | 200, `image/jpeg`, 50 by 75 pixels (a thumbnail, with a `._SY75_` size token in the name) |
| `GET` of the same URL without the size token | 200, `image/jpeg`, 1000 by 1500 pixels, no redirect, on `i.gr-assets.com` |

Consequences: Goodreads adds a title, authors, a cover, a series name inside
the title text and a Goodreads ID, and nothing that names an edition. A title
hit is a work-level candidate, so its edition fields start unselected like any
title match, and an ISBN hit is edition level and carries the ISBN that was
asked for. The cover host `i.gr-assets.com` is on the cover allow-list with
no `Referer`, and a URL for the "no photo" placeholder is not offered.

The endpoints are not an API Goodreads documents, and Goodreads has ended its
public API, so they can change without notice. A change shows as the source
failing, never as a wrong write: a hit with no usable ID or title is skipped.

## WeRead

Studied revision: `Tencent/WeChatReading` at
`d98e66ee60a8c7bcd35da549d0643c4470439643` (Apache-2.0), the `SKILL.md`,
`search.md` and `book.md` files. It documents the agent gateway Tencent
publishes for API-key clients. The source is written in Go from that
documentation and the measurements below, with no code taken from it.

| Item | Detail |
| --- | --- |
| Endpoint | `POST https://i.weread.qq.com/api/agent/gateway`, JSON body with `api_name`, the parameters flat beside it and `skill_version` (`1.0.5`, from `SKILL.md`; the README says `1.0.4`) |
| Auth | `Authorization: Bearer <key>`; the key is bound to the user's account |
| Used | `/store/search` (`keyword`, `scope` = 10 for e-books, `count`) and `/book/info` (`bookId`) |

Live measurements, status codes and key names only, made with a key of the
user's own:

| Request | Result |
| --- | --- |
| `/_list` | 200. Keys: `errcode`, `usage`, `request_format`, `apis` (18 entries with `api_name`, `description`, `need_login`, `params`, `response_fields`) |
| `/store/search` by a title, `scope` 10 | 200. Keys: `sid`, `hasMore` (an int), `results`. Each result is a group (`title`, `scope`, `scopeCount`, `currentCount`, `type`, `books`) holding one book here, so five books came back as five groups, all with `scope` 17 for a request of 10. Each book has `bookInfo` with `bookId` (a string), `title` and `author`, and no `isbn` or `publisher` |
| `/store/search` with an ISBN as `keyword` | 200, but the books returned were unrelated to that ISBN, so an ISBN is not a usable search term |
| `/book/info` | 200. Keys: `bookId` (a string), `deepLink`, `readerLink`, `title`, `author`, `cover`, `newRatingCount`, `intro`, `newRating`, `category`, `publisher`, `newRatingDetail`, `isbn`, `publishTime`. `isbn` was an empty string for the entry measured; `publishTime` is a string like `2022-04-01 00:00:00`; `category` is a path joined with `-`; there is no `translator` key. No `upgrade_info` key and `errcode` 0 appeared in any response |

Consequences: a lookup has to search by title, then read each plausible hit
with `/book/info`; an entry often has no ISBN, so it is named by its WeRead
book ID and treated like a Douban title hit; and the search groups must all be
read, with no filter on a group's `scope`.

`SKILL.md` tells an agent to stop and follow `upgrade_info.message` when that
field appears. That is instruction text from the server, so zlib only notices
that the field is present and reports a fixed message.

A lookup through zlib itself (not a script) with only `weread` enabled, a title
and one author, returned three candidates and no errors: the gateway accepts
zlib's own `User-Agent` and request body. Two candidates carried an ISBN and
one only its WeRead book ID, each was edition level, and the category path
arrived as two tags, one of which (a WeRead shelf name) is not a subject.

Not measured: how the gateway reports an invalid key (zlib treats HTTP 401 and
403 as one), its rate limits, and whether another account's results differ.

## ISBNs recorded by a download

The Z-Library record's ISBN is the cheapest way to fix a Chinese book's
metadata, since a title search finds little. It is not trustworthy enough to
write as the file's ISBN unchecked: a written ISBN is read as exact by every
later lookup, and records and files do carry the ISBN of another book. So:

- It is written as `isbn` only when the file has no ISBN, the record's
  title is at least 0.8 similar to the file's (the matcher's own threshold is
  0.5, since a write is permanent), and the record names one edition. Z-Library's
  `identifier` sometimes lists the ISBNs of several editions (about 12% of the
  results of one measured search, 3% of one history page), and a title cannot say
  which of them the file is. An ISBN-10 counts as the ISBN-13 it converts to, so a
  book's own pair is one edition.
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
