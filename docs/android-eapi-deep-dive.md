# Z-Library Android EAPI: Deep Dive

Endpoint, model and sorting analysis of the official Android APK (`com.positron_it.zlib`, `versionName=1.11.4`, `versionCode=21`, SHA-256 `159a142595ab2f6be62a4b231d5d6193739607bfe1d816271bf52f217de32120`) from the [APK download URL](https://s3proxy-alp.cdn-zlib.sk/swfs_second_public_files/soft/android/zlibrary-app-latest.apk), checked against live read-only EAPI responses. `latest.apk` is a mutable URL; check the SHA-256 before comparing.

## Evidence

| Tag | Source | What it proves |
| --- | --- | --- |
| A | Retrofit annotations and models in `classes.dex`, exported with [JADX 1.5.6](https://github.com/skylot/jadx/releases/tag/v1.5.6) | Paths, HTTP methods, fields and call chains the APK **declares**; not that the server accepts them or that the app calls them. |
| L | Read-only EAPI requests on `https://z-lib.gd` with an authorized session | Responses for one account and domain. Only success codes, key names, counts and ID-order comparisons are recorded; no credentials, account data, titles, links or raw bodies. Not an Android traffic capture. |
| R | zlib's [client](../internal/zlib/eapi.go), [commands](../internal/cli/root.go) and [EAPI notes](eapi.md) | What zlib implements. |

`ZLibApi` has 37 methods. To reproduce A:

```powershell
& '<jadx-dir>injadx.bat' --single-class 'com.positron_it.zlib.data.ZLibApi' --single-class-output '<temp-dir>ZLibApi.java' '<temp-dir>zlibrary.apk'
```

Models are in the `data.models` classes of the same package. The APK's fixed authorization value, raw headers and reusable credentials are deliberately not recorded.

## Retrofit endpoint matrix

"Measured" means L. `—` means not tested, not unavailable. `success=1` does not prove the expected fields exist or that sorting took effect. `{id}:{hash}` is zlib's book reference notation, not a literal colon in the path.

| APK method, HTTP path | Parameters and APK response model | Measured (L) | zlib status or recommendation |
| --- | --- | --- | --- |
| `login` · `POST /eapi/user/login` | Form `email,password,auth_token`; `ZLibUserListing` | —; the [EAPI notes](eapi.md) record that some accounts are refused | `login` is wired up; Cookie import stays. Report the server's error on refusal. |
| `searchBooks` · `POST eapi/book/search` | Form `message,yearFrom,yearTo,languages[],extensions[],e,page,limit,order`; `ZLibBooksListing` | All eight APK sort values returned `success=1`, each with a different order of the first 10 IDs for the same query; see the next section | `search --order` uses these eight values. |
| `singleBook` · `GET /eapi/book/{id}/{hash}` | Path ID/hash; `ZLibBookSingle` | Details of one saved book: `success=1` with a `book` object | `FetchBook` is wired up. |
| `fileFormats` · `GET /eapi/book/{id}/{hash}/formats` | Path ID/hash; `ZLibBookFormats` | Six different saved books all returned `success=1, books=[]` | A `formats` query could be added, but an empty list must be shown as such; never promise alternative formats. |
| `singleBookRecommendations` · `GET /eapi/book/{id}/{hash}/similar` | Path ID/hash; `ZLibMinBookListing` | One call returned `success=1` and 24 reduced books | Worth adding `similar <id:hash>`. |
| `getDownloadLink` · `GET /eapi/book/{id}/{hash}/file` | Path ID/hash; `ZLibFileListing` | —; it generates a download link, so it was not triggered | `download` is wired up; the redirect target still needs validation. |
| `downloadFile` · dynamic `GET @Url` | `@Streaming`, binary response | — | A file stream, not another fixed EAPI path. |
| `booksMostPopular` · `GET /eapi/book/most-popular` | Credential headers; `ZLibMinBookListing` | `success=1`, 100 books | `most-popular` is wired up. |
| `booksRecentlyAdded` · `GET /eapi/book/recently` | Credential headers; `ZLibMinBookListing` | **Returned only `success=1`, no `books`** | Do not treat `recently` as a usable list yet; confirm the response contract first. |
| `booksRecommendations` · `GET /eapi/user/book/recommended` | Credential headers; `ZLibMinBookListing` | `success=1`, 100 books | `recommended` is wired up. |
| `getDownloadedBooks` · `GET /eapi/user/book/downloaded` | `order,page,limit`; `ZLibTypedBooksListing` | `page=1,limit=2` returned 2 books plus pagination; for the first 10 books the seven APK sort values each gave a different ID order | `history` is wired up; sorting could be added. |
| `getSavedBooks` · `GET /eapi/user/book/saved` | `order,page,limit`; `ZLibTypedBooksListing` | `page=1,limit=2` returned 2 books plus pagination; the 8 saved books came back in a different ID order for each of the seven APK sort values | `favorites` is wired up; sorting could be added. |
| `userProfile` · `GET /eapi/user/profile` | Credential headers; `ZLibUserListing` | `success=1`, with account and quota fields; the response has sensitive fields, whose values were not recorded | `profile` is wired up; JSON must keep omitting credentials. |
| `getHosts` · `GET /eapi/info/domains` | Credential headers; `ZLibHostsListing` | `success=1`, 5 entries in `domains` | Used by `doctor`; the list is not every working domain worldwide, so each must be probed. |
| `getServiceInfo` · `GET /eapi/info/service` | `ZLibServiceInfo` | `success=1`; `service` contains `desktopApps,supportEmail` | Could be a read-only `info service`; do not confuse it with domain health. |
| `getSiteInfo` · `GET /eapi/info` | Credential headers; `ZLibInfo` | `success=1`; top level has `articles,banner,books,extensions,languages,plans,service` | Could back a summary `info`; the real response has more fields than the APK model declares. |
| `getLanguages` · `GET /eapi/info/languages` | `ZLibBooksLanguages` | `success=1`; `languages` is a name map | Could supply server-side search filter values. |
| `getExtensions` · `GET /eapi/info/extensions` | `ZLibBooksExtensions` | `success=1`, 12 entries in `extensions` | Could supply server-side search filter values. |
| `getPlanLimitations` · `GET /eapi/info/plans` | `ZLibPlanLimitListing` | `success=1`; `plans` contains `basic,limits,premium` | Could add `info plans`; do not mix it up with the user's remaining quota. |
| `getDonations` · `GET /eapi/user/donations` | Credential headers; `ZLibDonationsListing` | `success=1`; top level is `success,donations`; record values were not read | Holds personal donation records; evaluate CLI need and privacy separately. |
| `getHostsByUrl` · dynamic `GET @Url` | Credential headers; `List<ZLibHost>` | — | The APK uses it as an extra domain source; a user-supplied URL would need strict validation. |
| `testConnection` / `testConnectionPersonal` · dynamic `GET @Url` | Target URL under test | — | Internal network probing in the APK; must not be exposed for arbitrary URLs. |
| `loginPersonal` · dynamic `POST @Url` | Form `action,password,auth_token`; `ZLibUserListing` | — | Personal entry flow; first work out how the URL is obtained and how it relates to auth. |
| `tokenSignIn` · `POST /eapi/user/token-sign-in` | Form `id_token,name`; `ZLibUserListing` | — | Third-party token login; the declaration does not show it works. |
| `registration` · `POST /eapi/user/registration` | Form `email,password,name`; `ZLibUserListing` | — | Registration creates external state; not tested. |
| `registrationConfirm` · `POST /eapi/user/email/confirmation` | Form `email,password,name,code`; `ZLibUserListing` | — | Email confirmation flow; not tested. |
| `registrationResend` · `POST /eapi/user/email/confirmation/resend` | Form `email,password,name`; `ZLibUserListing` | — | Same path as the next method but a different request encoding. |
| `resendConfMail` · `POST /eapi/user/email/confirmation/resend` | Credential headers; `ZLibStatus` | — | The two Retrofit methods cannot be merged by path alone. |
| `sendConfirmationCode` · `POST /eapi/user/send-code` | Form `verifyAction,email,name,verifyCode`; `ZLibUserListing` | — | May send email; not tested. |
| `recoverPassword` · `POST /eapi/user/password-recovery` | Form `email,verifyCode`; `ZLibUserListing` | — | Password recovery flow; not tested. |
| `updateProfile` · `POST /eapi/user/update` | Credential headers; form `email,password,name,kindle_email,verifyCode` | — | Changes account information; not tested. |
| `saveBook` · `GET /eapi/user/book/{bookId}/save` | Numeric book ID; `ZLibStatus` | — | Uses GET but changes favorites; must not be treated as a read. |
| `deleteSavedBook` · `GET /eapi/user/book/{bookId}/unsave` | Numeric book ID; `ZLibStatus` | — | Removes a favorite; not tested. |
| `deleteDownloadedBook` · `GET /eapi/user/book/{id}/delete` | Numeric book ID; `ZLibStatus` | — | Deletes a history entry; not tested. |
| `sendToMail` · `GET /eapi/book/{id}/{hash}/send-to-{type}` | Path ID/hash/type; `ZLibStatus` | — | The server sends the mail; not the same as the CLI's local SMTP Kindle delivery. |
| `onBannerHide` · `GET /eapi/user/hide-banner` | Credential headers; `ZLibStatus` | — | Changes remote display state; unsuitable as a diagnostic request. |

Several paths change the account or send email despite using GET, so the read-only probes skip them. `DataModule` also packages clearnet and onion entry points; they do not replace `doctor` probing.

## Data models and call behaviour

| Model | APK JSON fields or behaviour | CLI impact |
| --- | --- | --- |
| `ZLibBooksListing` | `success,books,exactBooksCount,pagination`; each search item is a `ZLibBook` | `exactBooksCount` differs from the pagination total; make the meaning explicit before displaying it. |
| `ZLibBook` | `id,hash,title,author,year,extension,filesize,description`, plus `kindleAvailable,_isUserSavedBook,sendToEmailAvailable,readOnlineAvailable,readOnlineUrl` and others | Book details could show the server's capability flags; fields may be missing. |
| `ZLibMinBookListing` / `ZLibMinBook` | Top level `success,books`; each item in the APK model has only `id,title,author,cover,hash` | Popular, recommended and similar results cannot be assumed to carry format, size or year; fetch details. The measured similar-books response also had fields the model does not declare. |
| `ZLibTypedBooksListing` / `ZLibListBook` | Saved and downloaded share `success,books,pagination`; items are richer than the reduced version | Pagination and display can be shared, but each list keeps its own sort semantics. |
| `ZLibPagination` | `before,next,limit,current,total_items,total_pages` | Page numbers follow the server's `current/total_pages`. |
| `ZLibBookFormats` / `ZLibFormat` | Top level `success,books`; format items have `id,hash,extension,filesize,filesizeString` | Formats can have different ID/hash pairs; changing the download file extension is not enough. An empty `books` is a valid measured result. |
| `ZLibFileListing` / `ZLibFile` | `file.downloadLink,description,author,extension,allowDownload` | Check `allowDownload` before downloading, then validate the dynamic URL. |
| `ZLibHostsListing` / `ZLibHost` | Each `domains` entry has `domain,contentAvailable,premium,isRedirector` | Returned domains need checking against the current network, availability and redirects; `contentAvailable` is not local measured health. |
| `ZLibUserListing` / `ZLibUser` | Username, email, Kindle email, donation and download quota, confirmation state, Premium and more | `profile` should only output what is shown to the account owner, and must not expose credentials in logs or JSON. |
| `ZLibPlanLimitListing` / `ZLibPlans` | `plans.basic,plans.premium,plans.limits` | Plan rules are public configuration; show them apart from the personal `downloads_today/downloads_limit`. |

`ZLibBooksRepo` passes the filters, page and `order` from `BooksRepo.Query` to `ZLibApi.searchBooks`, so the sort value reaches the form. The app caches popular, recent and recommended in Room tables, and sends `order,page,limit` for favorites and download history. A cached list in the app does not mean the server still returns one: `/recently` returns only a success flag.

Most account methods declare the `remix-userid` / `remix-userkey` headers; `HeaderInterceptor` adds a Cookie with the same names, the app version, the language and a fixed authorization header. zlib authenticates with its own session and Cookie import. Whether any extra header is needed must be shown by server responses, not inferred from decompiled code, and APK constants must never be copied into source.

## Sorting: APK declarations, call sites and server effect

The Android search sort dialog hands the option index to an array in `com.positron_it.zlib.ui.library.viewModel.a`; `ZLibBooksRepo` then passes it through `BooksRepo.Query.order` into the `order` form field of `searchBooks`. The APK's eight values, in order:

| UI meaning | `order` value |
| --- | --- |
| Popular | `popular` |
| Best match | `bestmatch` |
| Recently added | `date` |
| Title A–Z | `titleA` |
| Title Z–A | `title` |
| Year | `year` |
| File size, largest first | `filesize` |
| File size, smallest first | `filesizeA` |

For the same query with `limit=10`, the eight values produced eight different ID orders; `date_created`, `date_updated` and a deliberately invalid value all matched `popular`. The values therefore change the order, but a ten-book sample does not prove that year and file size sort strictly. `search --order` offers these eight values; the `date_created` and `date_updated` constants remain only for existing callers. A success code alone does not verify sorting.

Favorites and download history use a different set, from `com.positron_it.zlib.ui.mybooks.viewModel.g`: favorites `saved_date,saved_dateA,titleA,title,year,filesize,filesizeA`, history `downloaded_date,downloaded_dateA,titleA,title,year,filesize,filesizeA`. Each of the seven values gave a different ID order for 8 saved books and for the first 10 history entries; `date_created` and an invalid value fell back to the default order, so acceptance by the server does not prove a value is valid. These lists must not share the search sort enum.

## Command recommendations

Ordered by observed server behaviour, user value and implementation risk. Existing commands (`recommended`, `most-popular`, `favorites`, `profile`, `history`, `doctor`, `search`, `download`) are not repeated.

| Priority | Command or improvement | Basis and implementation limits |
| --- | --- | --- |
| Done | `search --order`, plus a sort choice in interactive search | The search form and CLI use the APK's eight values; without a chosen sort no `order` is sent. `date_created/date_updated` are not search options. |
| 1 | `similar <id:hash>` | `/similar` returned 24 books and fits the existing list and download screens; reduced books guarantee only ID/hash/title/author/cover. |
| 1 | `favorites --order` / `history --order` | The APK has seven values for each, and the existing EAPI methods only paginate; the ID-order comparison in L supports that these values really change results. Favorites and history must not reuse the search sort array. |
| 2 | `info languages`, `info extensions` | L observed a language map and 12 formats; search filter values could move from fixed local enums to server lists, still handling offline use and cache expiry. |
| 2 | `info service`, `info plans`, or a combined `info` | These three read-only endpoints return structured JSON. `plans` shows plan rules and `profile` shows the user's quota; service info can show only the meaningful fields. |
| 3 | `formats <id:hash>`, or a format choice before download | The APK has a full model, but all six measured books returned an empty array; report results as they are, without promising alternatives. |
| Deferred | `recently` | Returns `success=1` with no `books`; find out whether it needs other context or is no longer served. |
| Separate evaluation | `save`, `unsave`, `history delete`, server `send-to-*`, account update | All change server state or send mail, even the ones that use GET. They need explicit user actions and an error recovery design, and must not be mixed into health checks. |

The login, domain and Cookie limits in the [EAPI notes](eapi.md) still apply. The 5 entries from `/eapi/info/domains` are candidates for one account; only `doctor`, which requests each and validates JSON, redirects and the auth boundary, shows which work. The measurements do not cover anonymous access, other accounts or regions, or the app's runtime behaviour.
