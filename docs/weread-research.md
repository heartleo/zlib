# WeRead import limits

[README](../README.md) · [Send to WeRead](weread.md)

Facts about importing local books into WeRead. The values are
defaults; WeRead can change any of them on its server without an app update.

## Phone app

### File size

A file is rejected when it is larger than the limit for its format.

| Format | Limit |
| --- | --- |
| TXT | 20 MiB |
| PDF | 300 MiB |
| EPUB, MOBI, AZW3, DOC, DOCX | 300 MiB |

### Formats

TXT, PDF, DOC, DOCX, AZW3, MOBI and EPUB are accepted.

### Import count

- Accounts without a paid membership cannot import when the imported books on the
  shelf plus the books being added would exceed 200.
- Paid members are exempt from this limit.
- The count is the number of imported books currently on the shelf, including
  books still waiting to upload (IDs starting with `LOCAL_BOOK_`) and uploaded
  books (IDs starting with `CB_`). Removing an imported book frees room.

### Shelf total

The shelf has its own cap, 500 books by default. Paid members have no shelf
limit.

### Import entries

The app imports from WeChat, from local storage, and from a computer through the
WeRead web page.

### Paid-member features

- Importing without a count limit.
- Having no shelf limit.
- Read-aloud for imported books.

## E-ink app

- The app does not upload books from the device. Its import screens tell the user
  to import from a phone or from a computer. Its cloud-storage upload code is
  used for images and feedback attachments only.
- It has no import-count limit and no per-format file-size limits.
- The shelf cap is 500 books by default. Paid members are exempt. Over the cap, a
  single book that has already been purchased can still be added; any other add
  is refused with a message asking the user to tidy the shelf.
- Read-aloud for imported books requires a paid membership.
- It is signed with the same certificate as the phone app. Its package is
  `com.tencent.weread.eink`, and it requires Android 7.0 or later.

## Import protocol

The phone app and the e-ink app share these requests.

1. **Duplicate check (optional).** `POST /cos/receive` with the file's MD5, the
   file name, a timestamp, a random number and an anti-replay signature, or with
   a book ID instead of the MD5 and name. If the server already has the file, the
   reply carries the book and no upload is needed; the app treats it as "already
   exists".
2. **Upload credential.** `GET /cos/getcredential`. The query parameters are
   `name`, `suffix`, `pictype`, `cbid` and `scene`, all optional; a second form
   takes only `mode` and `suffix`. The reply holds `Response` (temporary
   credentials with start and expiry times), `ServerTime`, `ObjectName`,
   `bucket`, `region` (default `ap-shanghai`), `PicHost`, `timestamp` and
   `extraInfo`.
3. **Upload.** The file goes to Tencent Cloud Object Storage over HTTPS, in the
   region the reply names, either as one object or as a multipart upload that
   lists existing parts so an interrupted upload can resume.
4. **Notify.** `POST /cos/notify?name=<file name>&path=<ObjectName>&cancel=<0|1>`
   with an empty JSON body. `cancel=1` abandons the import; the app sends it when
   the user removes the book before it finishes.

### Notify status

| status | Meaning | Client behavior |
| --- | --- | --- |
| `0` | Completed | Stop; the book is imported |
| `1` | Processing | Ask again |
| `2` | Parse error | Stop; failed |
| `3` | Already exists | Stop; the reply carries the existing book ID, which the app adds to the shelf |
| `-1` | No status in the reply | Ask again |

The app asks again every 2 seconds, up to 90 times.

### Cloud-storage signature

Requests use the Tencent Cloud signature with algorithm `sha1`:

- `keyTime` is the credential's start time and expiry time, `start;end`.
- `signKey` is the lowercase hex of HMAC-SHA1 of `keyTime`, keyed by the temporary
  secret key.
- The string to sign is `sha1\n<keyTime>\n<hex SHA-1 of the HTTP string>\n`, where
  the HTTP string is the lowercase method, the path, the sorted query parameters
  and the sorted headers, each on its own line, with names lowercased and values
  percent-encoded.
- The signature is the lowercase hex of HMAC-SHA1 of the string to sign, keyed by
  the hex `signKey`.
- The authorization value lists `q-sign-algorithm`, `q-ak`, `q-sign-time`,
  `q-key-time`, `q-header-list`, `q-url-param-list` and `q-signature`, and the
  request also carries `x-cos-security-token`.
- The cloud-storage SDK requests sign only the `Host` header.

## Server-side rules

The client keeps no monthly counter. A rule that depends on time, such as a
number of imports per month, is decided by the server when the upload credential
is requested:

| errCode | Meaning |
| --- | --- |
| `-2999` | Import limit reached. The server supplies the message, which the app shows unchanged |
| `-2014` | Rate limited. The app stops its upload queue |

The three-books-per-30-days values in the phone app's settings only build the
"recent imports" group on the shelf-select screen. They are not a quota.
