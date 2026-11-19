# zlib meta reference

The CLI forms of the [metadata skill](../SKILL.md). Run them without a TTY: a
bare `zlib meta` or `zlib meta "<file>" --fetch` without `--json` opens a terminal
page an agent cannot drive.

## Read

```sh
zlib meta "<file>" --json
```

Prints `file`, `format` and the metadata: `title`, `title_sort`, `authors`,
`author_sort`, `publisher`, `languages`, `tags`, `series`, `series_index`,
`identifiers`, `published`, `description`, `rating` and `cover` (the embedded
cover's media type). `zlib meta "<file>" --get-cover cover.jpg` saves the cover,
and `--to-opf book.opf` saves the metadata as an OPF file.

## Look up

```sh
zlib meta "<file>" --fetch --json
zlib meta "<file>" --fetch --json -t "Dune" -a "Frank Herbert" --isbn 9780441013593
zlib meta "<file>" --fetch --json --source openlibrary,google
```

Returns `current` (what the file holds), `query` (what was searched), an
`extraction` object (what the file name and the file offered, and where each part
of the query came from), ranked `candidates` and any per-source `errors`. Each
candidate has `match` (`isbn` or `title`), `score`, `sources`, the metadata
fields, `cover_url` and `suggested_file_name`. `--fetch` writes nothing and
cannot be combined with the field options.

`-t`, `-a` (authors separated by `&`) and `--isbn` set what to search for.
`--source` picks the catalogues (`openlibrary`, `google`, `goodreads`, `douban`,
`weread`) for one run. Without it a lookup follows `ZLIB_METADATA_SOURCES`, or asks Douban for
a Chinese book and Open Library for any other, plus Google Books when a key is
set. `weread` sends the title to Tencent, so name it only when the user asked.
`goodreads` is never asked unless the user turned it on: leave it out unless the
user wants it.

Exit codes: `1` nothing matched, `2` an invalid option, `4` every source was
unreachable.

## Write

```sh
zlib meta "<file>" -t "Dune" -a "Frank Herbert & Brian Herbert" --json
zlib meta "<file>" --isbn 9780441013593 --date 2005-08-02 --tags "Fiction, Classics"
zlib meta "<file>" -s "Dune Chronicles" -i 1 -p "Ace Books" -r 4.5 --language en
zlib meta "<file>" --identifier google:abc123 --identifier "uuid:"
zlib meta "<file>" --cover cover.jpg
```

| Option | Sets |
| --- | --- |
| `-t`, `--title-sort` | the title, the title as a library sorts it |
| `-a`, `--author-sort` | the authors (separated by `&`), the authors as a library sorts them |
| `-p`, `-l` (`--language`), `--tags` | the publisher, language codes, tags (comma-separated) |
| `-s`, `-i` | the series, the position in it |
| `--isbn`, `--identifier scheme:value` | an ISBN, any identifier (`scheme:` removes it) |
| `-d`, `-r`, `-c` | publication date, rating 0 to 5, description |
| `--cover FILE`, `--from-opf FILE` | cover image, every field of an OPF file |

A stderr line ending in `skipped.` names fields the format cannot store. A value
the editor would refuse (a rating above 5, a bad date, an invalid ISBN) exits `2`.
The file is rewritten in place and `--json` prints the metadata it now holds.

## Rename

```sh
zlib meta "<file>" --rename --json
zlib meta "<file>" -t "Dune" -a "Frank Herbert" --rename
zlib meta "<file>" --rename-to "Dune - Frank Herbert"
```

`--rename` names the file from the title and authors a lookup would search for (the
file's own when the same command sets `-t`, `-a` or an OPF), in the user's file
name format: `Title - Authors.ext` by default, `Authors - Title.ext` or
`Title (Authors).ext` through `ZLIB_FILENAME_FORMAT`, or a custom pattern.
`--rename-to` gives a name. Both keep the folder and the extension, never replace
another file, and report `file` and `renamed_from` with `--json`. `--fetch --json`
adds a `suggested_file_name` to each candidate without renaming.

## Settings

`zlib config metadata` shows the user's metadata settings: the lookup sources, the
number of results, the file name format, and the Google Books and WeRead API keys.
Ask the user to change them; do not read or print the keys.
