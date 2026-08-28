---
name: kindle
description: Send a downloaded book file to a Kindle via the zlib CLI, and walk the user through the one-time SMTP/Amazon setup it needs. Use when the user wants to deliver an ebook to their Kindle, configure Kindle delivery, or fix a failed Kindle send.
license: MIT
---

# Kindle delivery

Deliver an already-downloaded file to the user's Kindle through the `zlib` CLI, and handle the one-time configuration it depends on. To find or download the book first, use the `zlib` skill; this skill takes it from a local file to the Kindle.

## Preflight

Confirm that `zlib` is on `PATH` with the current shell (`command -v zlib` on POSIX or `Get-Command zlib` in PowerShell). Sending itself doesn't need a Z-Library session, only Kindle config. If the binary is missing, tell the user to install it with `go install github.com/heartleo/zlib/cmd/zlib@latest`.

## Send — `zlib kindle send <file>`

**Always pass the file path** — bare `zlib kindle send` opens an interactive file picker you can't answer. With a path and no terminal it sends directly and exits, printing success or a clear SMTP error:

```bash
zlib kindle send ./books/dune.epub
```

`zlib download <id> --send-to-kindle` delivers in one step once Kindle is configured.

## First check the config

Before sending, read `~/.config/zlib/kindle.json` and check `ZLIB_SMTP_PWD`. If the file is missing, holds placeholder-looking values (e.g. `mykindle@kindle.com`, `sender@gmail.com`), or the password is unset, run the setup walkthrough below before sending. If it looks configured, just send and relay the result.

## Configuring Kindle delivery — walk the user through it in conversation

**Guide the user step by step — ask one question at a time and wait for each answer**, then write the non-secret config with the available filesystem or shell tool. The user only edits the secret env file in step 4.

1. **Ask for the Kindle receive address.** "What's your Kindle address (`xxx@kindle.com`)? Find it under Amazon → Manage Your Content and Devices → Preferences → Personal Document Settings." It must be the Amazon-assigned address, not their normal email.
2. **Ask for the sender email.** Remind them it must be on Amazon's approved-sender list (same Personal Document Settings page) — otherwise Amazon silently drops the file even though SMTP reports success.
3. **Derive SMTP from the sender domain** — gmail.com → `smtp.gmail.com:587`, outlook/hotmail → `smtp-mail.outlook.com:587`, qq.com → `smtp.qq.com:587`, 163.com → `smtp.163.com:465`; anything else, ask. Then write the config yourself (create the directory if needed):

   ```json
   // ~/.config/zlib/kindle.json
   {"to":"<kindle-address>","from":"<sender-email>","smtp_host":"<host>","smtp_port":<port>}
   ```

4. **Set the SMTP app password — the user edits the env file directly. Never ask for, paste, or echo the password in the chat, and never put it on a command line.** zlib reads `ZLIB_SMTP_PWD` from `~/.config/zlib/.env` at startup. Prepare the file and hand the user the exact path plus a one-click open command, then wait for them to confirm they saved it.

   Create the file (if missing) with a template line and print its absolute path. Use the commands for the current shell.

   POSIX shell:

   ```bash
   mkdir -p ~/.config/zlib
   [ -f ~/.config/zlib/.env ] || printf '# zlib secrets — one KEY=value per line\nZLIB_SMTP_PWD=\n' > ~/.config/zlib/.env
   chmod 600 ~/.config/zlib/.env
   echo "Open and edit: $(cd ~/.config/zlib && pwd)/.env"
   ```

   PowerShell:

   ```powershell
   $configDir = Join-Path $env:USERPROFILE '.config\zlib'
   New-Item -ItemType Directory -Force $configDir | Out-Null
   $envFile = Join-Path $configDir '.env'
   if (-not (Test-Path $envFile)) {
     Set-Content -Encoding utf8 $envFile "# zlib secrets — one KEY=value per line`nZLIB_SMTP_PWD="
   }
   $envFile
   ```

   Tell the user to set `ZLIB_SMTP_PWD=<app-password>` on its own line and save. It must be an **app password**, not the account password — for Gmail: enable 2FA, generate at <https://myaccount.google.com/apppasswords> (16 chars; strip spaces). Give them a ready open command for their OS:

   - Windows: `notepad "$env:USERPROFILE\.config\zlib\.env"`
   - macOS: `open -t ~/.config/zlib/.env`
   - Linux: `${EDITOR:-nano} ~/.config/zlib/.env`

Once the config and the saved password are in place, run the send and relay the result.

## Errors

- **`535` / `454` SMTP error** — almost always a real account password (or none) instead of an app password, or the sender isn't allowed. Re-check step 4.
- **SMTP connection timeout** — the network can't reach the SMTP host (e.g. Gmail blocked). `ZLIB_PROXY` does *not* cover SMTP (it's a direct connection); the user needs a system-level VPN/TUN, or should switch to a reachable provider (QQ/163). Probe with `Test-NetConnection <host> -Port <port>` in PowerShell or `timeout 8 bash -c 'echo > /dev/tcp/<host>/<port>'` on POSIX.
- **"Sent" but nothing arrives** — the sender email isn't on Amazon's approved-sender list (step 2).
- **Password reported missing after editing `~/.config/zlib/.env`** — check no stale `ZLIB_SMTP_PWD` in a *working-directory* `.env` is overriding it (working-dir wins over the global file); have the user fix whichever file applies. Older zlib builds load only the working-directory `.env`, so on those the user must put `ZLIB_SMTP_PWD=` in the `.env` of the directory the command runs in, and should upgrade.
