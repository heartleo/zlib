# Editions and works

A **work** is the book as an idea: its title, authors, plot, series. An
**edition** is one published form of it: a publisher's hardcover, a paperback
reprint, an ebook, a translation, a revised text. They share a work and differ in
almost everything a file stores about publication.

## What belongs to which

| Describes the work | Describes the edition |
| --- | --- |
| Title (in its original language), authors | Publisher or imprint, edition statement |
| Series and, when the record states it, the position | Publication date of this edition |
| Description, subjects, tags | ISBN, ASIN and other edition identifiers |
| The first publication year of the work | Language of this text, translator |
| | Format, page count, cover |

A catalogue row is **edition-level** (it names one ISBN, publisher and date) or
**work-level** (it knows the work and, at most, its first year). Take the
edition's fields only from an edition-level row that you have tied to this
edition. A work-level row can fill the work's fields and nothing else.

## Telling the cases apart

- **The same edition.** An identifier agrees, or the publisher, the edition
  statement, the format and the date all agree with what the file or its copyright
  page says. This is the only case in which every field is safe.
- **A reprint.** The same text, the same publisher or a neighbouring one, a year
  within a couple of years. The work's fields are safe; keep the file's own date
  unless you can show it is wrong.
- **A revised edition.** "Second edition", "revised and updated", a new preface or a
  different page count. The same work, different content. Do not take the edition
  fields of one for the other.
- **A translation.** Same work, another language, another title, usually another
  publisher, a different year. Take the work's fields, keep the file's language, and
  check that the translator is not listed as an author.
- **Another format.** Hardcover, paperback, ebook and audiobook have their own ISBNs.
  Never borrow an identifier across formats.
- **Another volume.** The same series stem with a different number ("Dune 01" and
  "Dune 02", a volume numbered with a Roman numeral, a Chinese 第2部 or 卷). It is a different
  book. A number that is part of the title ("1984", "Fahrenheit 451") is not a volume.
- **A sequel or companion.** A title that continues the wanted one with more words
  and no subtitle ("Dune" and "Dune Messiah", a cookbook or a guide named after the
  book). Different book, same author often: do not accept it on author and title
  overlap.
- **A collection or box set.** It lists several works and several ISBNs. Its ISBN is
  not the ISBN of one volume in it, and its description covers all of them. Match a
  single volume to a single volume.
- **A derivative.** A summary, study guide, workbook, analysis or reading guide is a
  book about the book. It shares the title's words and none of its content.
- **A different author.** The same surname with another given name is another
  person, not a variant spelling.

## Across languages

A file in one language often meets catalogues that know the book under another title.

1. Search the title as the file has it, then the original-language title, then a
   well-known translated title. Keep the authors fixed: the name is usually the same
   or a standard transliteration.
2. Compare authors as people: reorder "Last, First", accept initials, and for Han
   names compare the characters (a Latin transliteration is the same person only
   when the source says so).
3. Titles in scripts without spaces (Chinese, Japanese) have no word boundaries:
   compare runs of characters, and read volume markers (第2部, 卷二, a numeral glyph)
   as numbers. A longer title that begins with the wanted one and adds no edition
   word is probably another volume.
4. Treat a hit in another language as the same work, never as the same edition.
   Take the work's fields and keep the file's language, publisher and date.
5. When the only link is a translated title, confirm with an independent signal:
   the same author, the same year range for the original, or a catalogue entry that
   links the translation to the original.

## Series and position

Take a series name and a position only from a record that states them. Do not
infer a position from publication order, and do not assume that a numbered title is
the volume number of a series. A series name with no position is useful on its own.

## When you cannot tell

Say which edition is unresolved and why, list the facts that would settle it (the
ISBN on the copyright page, the publisher's name, the edition statement, the
publication year of this printing), and take only the work's fields. Offering the
user two clearly labelled candidates is better than choosing one.
