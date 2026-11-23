# Evidence

How to weigh what a record says against what the file says. The aim is a
decision you can defend with facts, not a number.

## The signals

| Signal | Strong when | Weak or misleading when | A conflict means |
| --- | --- | --- | --- |
| **ISBN** | The record's ISBN equals the file's own, and the title and authors agree. | The ISBN came from a download, the copyright page or the file name (a hint), or the title disagrees. Files often carry another book's ISBN. | Another edition (same work, different ISBN) or a wrong ISBN in one of them. |
| **Other identifiers** (Goodreads, Douban, WeRead, Open Library IDs) | They name the same record in two sources. | They name an edition in one source and a work in another. | Different editions, or one source merged two. |
| **Edition facts**: publisher or imprint, edition statement, format, publication date | Several agree with the file. | Only the year agrees. A work-level row's year is the work's first year. | Another edition of the same work. |
| **Title** | The whole title agrees, or only a subtitle, bracket or edition word differs. | It agrees only because one title contains the other, or the words are common ("Death", "Origin"). | Another volume, a sequel, a companion, a summary or another book. |
| **Authors** | Same surname and the same or compatible given name (an initial agrees with the full name). | Only a surname agrees: it fits every person of that name. The author is a translator or editor in one source. | Another person of the same surname (two Herberts), or another book. |
| **Year** | Within two years of the file's year, taken as a reprint. | The file has no year, or the row is a work. | Another edition, often decades apart (a reprint, a new translation). |
| **Language** | The same primary language. | One side is unknown. | A translation, or another book. |
| **Series and position** | The series and the volume number agree. | The position came from publication order. | Another volume. |
| **Description, cover, tags** | They support a match already made. | They are the only thing that agrees. | Never decisive: covers and blurbs are reused across editions. |

Read three rules into every row:

- **A conflict outweighs an agreement.** Same title, authors and year but a
  different volume number is a different book. Same book but a different ISBN
  is a different edition.
- **Missing data is neutral.** No year, no language, no ISBN on one side counts
  for nothing, in either direction. Do not penalise a record for what the file
  does not say, and do not credit it for what it did not say either.
- **Independence matters.** Two sources that copy each other agree for one
  reason. A title read from the file name and an ISBN recorded by the same
  download are one piece of evidence, not two.

## Confidence

Use words with meanings, and say which one you used.

- **High**: an identifier ties the record to this exact edition and nothing
  contradicts it, or authoritative evidence does (the publisher's or a national
  library's record for this edition).
- **Medium**: title, authors and edition facts agree and nothing conflicts, but
  the edition is not certain. This is the usual state of a lookup without an ISBN.
- **Low**: the evidence is indirect, partial or in conflict. Offer it as a lead;
  do not take its edition fields.
- **None**: no reliable evidence. Say there is no match and stop. An empty answer
  is better than a plausible wrong one.

## Missing and conflicting information

| Situation | What to do |
| --- | --- |
| The file has no ISBN | Match on title, authors and year together. Take work-level fields; leave the edition fields. |
| The file has a wrong or foreign ISBN (the title disagrees) | Do not trust the ISBN. Match on title and authors; report the ISBN as suspect. |
| The file has no title (or a guessed one) | Search with the authors and year, and the file name's reading; confirm with the user. |
| The file's year differs from the record's by more than two | Another edition, or a record for the work. Keep the work's fields and leave the date. |
| Two sources give different years or publishers | Prefer the source tied to the exact edition (publisher, national library, ISBN agency). Show both if it stays open. |
| A record has several ISBNs in one field | They may be editions or formats (hardcover, paperback, ebook). Do not pick one for this file unless the file's own ISBN is among them. |
| The authors differ only by order, initials or "Last, First" | The same people. Compare names as people, not as strings. |
| The authors differ by a given name | Different people (a father and a son, two namesakes). |
| The title is in another language | Search the original and the translated title. Treat a hit as the same work, not the same edition. |
| Everything agrees but the language | A translation. Take the work-level fields; leave the language and publisher. |

## Two worked examples

**A file titled "Dune", author Frank Herbert, year 1965, no ISBN.** A search
returns "Dune" by Frank Herbert (2005), "Dune Messiah" by Frank Herbert,
"Dune" by Brian Herbert, and "Dune Study Guide". The first is the work: take the
title, authors and description; the 2005 date is another edition's, so leave the
date and publisher unless the 1965 edition turns up. The second extends the title
and is another volume. The third shares a surname but not the given name. The
fourth is a book about the book. State that the edition is unresolved and what
would settle it (an ISBN, the publisher, the edition statement on the copyright page).

**A file with ISBN 9780441013593 titled "Dune".** A record with that exact ISBN
and the title "Dune" is the book itself: take its fields. A record with the same
title and a different ISBN is the same work in another edition; do not take its
ISBN. A record with that ISBN and the title "Children of Dune" means the file's
ISBN is wrong: do not take that record's fields, and tell the user.
