# Z-Library Android 1.11.4 API: Static Analysis

Static analysis of the official Android APK (`com.positron_it.zlib`, `versionName=1.11.4`, `versionCode=21`, SHA-256 `159a142595ab2f6be62a4b231d5d6193739607bfe1d816271bf52f217de32120`) from the [APK download URL](https://s3proxy-alp.cdn-zlib.sk/swfs_second_public_files/soft/android/zlibrary-app-latest.apk). `latest.apk` is a mutable URL; check the SHA-256 before comparing. The [deep dive](android-eapi-deep-dive.md) adds the full endpoint matrix, models, sorting and live responses.

## Method and limits

The API is defined in `com.positron_it.zlib.data.ZLibApi`. [JADX 1.5.6](https://github.com/skylot/jadx/releases/tag/v1.5.6) exports `ZLibApi`, `DataModule` and `HeaderInterceptor`:

```powershell
& '<jadx-dir>\bin\jadx.bat' --single-class 'com.positron_it.zlib.data.ZLibApi' --single-class-output '<temp-dir>\ZLibApi.java' '<temp-dir>\zlibrary.apk'
```

The APK was not run and its signature was not verified. Decompiled annotations show what the app declares, not what it sends at runtime or what the server accepts.

## Declared endpoints

Session methods declare the `remix-userid` and `remix-userkey` headers. `HeaderInterceptor` also sends a Cookie with the same credentials, the Android version, the language, and a fixed authorization header constant, whose value is not recorded here.

| Function | Method and path | Declared parameters |
| --- | --- | --- |
| Login | `POST /eapi/user/login` | Form `email`, `password`, `auth_token` |
| Search | `POST eapi/book/search` | Form `message`, `yearFrom`, `yearTo`, `languages[]`, `extensions[]`, `e`, `page`, `limit`, `order` |
| Book details | `GET /eapi/book/{id}/{hash}` | Path `id`, `hash` |
| Download link | `GET /eapi/book/{id}/{hash}/file` | Path `id`, `hash`; the file is streamed by a second GET to a dynamic URL |
| Available formats | `GET /eapi/book/{id}/{hash}/formats` | Path `id`, `hash` |
| Similar books | `GET /eapi/book/{id}/{hash}/similar` | Path `id`, `hash` |
| Downloaded, saved | `GET /eapi/user/book/downloaded`, `GET /eapi/user/book/saved` | Query `order`, `page`, `limit` |
| Popular, recent, recommended | `GET /eapi/book/most-popular`, `GET /eapi/book/recently`, `GET /eapi/user/book/recommended` | Session headers |
| Profile | `GET /eapi/user/profile` | Session headers |
| Domains, service info | `GET /eapi/info/domains`, `GET /eapi/info/service`, `GET /eapi/info` | Some carry session headers |
| Languages, extensions, plans | `GET /eapi/info/languages`, `GET /eapi/info/extensions`, `GET /eapi/info/plans` | None |
| Change saved state | `GET /eapi/user/book/{bookId}/save`, `/unsave`, `GET /eapi/user/book/{id}/delete` | Book ID; GET, but changes state |
| Send by email | `GET /eapi/book/{id}/{hash}/send-to-{type}` | Path `id`, `hash`, `type` |

The API also declares registration, email confirmation, password recovery, `POST /eapi/user/token-sign-in`, `POST /eapi/user/update`, and a personal login against a dynamic URL. `DataModule` sets the Retrofit base URL to `https://zlibrary.se/` and adds an onion entry point; both are packaged defaults, not working domains.

## Relation to zlib

Every endpoint in the [EAPI notes](eapi.md) (login, search, domain check, profile, download history, book details, download link) has a matching path in `ZLibApi`, and the existing client already matches the annotations:

- `eapi.go`: search fields, form encoding and the `remix-userid` / `remix-userkey` headers.
- `eapi_book.go`: book details and the two-step download.
- `eapi_profile.go`: profile and download history paths.

The APK's extra headers (the fixed authorization constant and device information) are not shown to be required, and APK constants must never be copied into source. The packaged `DataModule` domain must not bypass zlib's domain allowlist or redirect credential stripping.

| APK evidence | Direction for zlib |
| --- | --- |
| Search form `order` field | Carry an optional sort through `SearchOptions`, the EAPI request and `--order`; send no `order` by default. |
| `/formats`: list of `id`, `hash`, `extension`, `filesize`, `filesizeString` | Let the user pick a format. Formats can have different ID/hash pairs, so changing the file extension is not enough. |
| `/user/book/saved`, `/save`, `/unsave` | Read-only favorites list first. The state changes use GET and must never be cached or retried as reads. |
| Popular, recent, recommended, similar | Reuse the `Book` list views, but these responses are reduced `books` lists; fetch details for missing fields. |
| `/info/languages`, `/extensions`, `/plans` | Could replace local filter enums with server values; check response shape, caching and anonymous access first. |

New endpoints need decoding and error paths tested against `httptest` fixtures, then a check against a real server on an authorized account. Changing favorites and sending email change external state and need explicit consent.
