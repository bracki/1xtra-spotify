# BBC 1Xtra → Spotify playlist generator (powered by Claude)

An interactive web app that scrapes the [BBC Radio 1Xtra playlist](https://www.bbc.co.uk/programmes/articles/2sgpCPqVPgjqC7tHBb97kd9/the-1xtra-playlist)
and rebuilds it as a Spotify playlist.

Instead of brittle CSS-selector + regex scraping, it hands the page HTML to
**Claude** (`claude-opus-4-8` via the official `anthropic-sdk-go`). Claude finds
the tracklist and normalises it into clean `Artist - Title` search queries — so
it keeps working even when the BBC changes their page layout.

## Setup

You need:

1. A **Spotify app** — create one in the
   [Spotify developer dashboard](https://developer.spotify.com/dashboard) and add
   `http://localhost:8080/callback` as a Redirect URI. Note the Client ID and
   Client Secret.
2. An **Anthropic API key** — from the [Anthropic console](https://console.anthropic.com/).

## Run

```
go run .
```

Then open <http://localhost:8080> and follow the three steps:

1. **Credentials** — paste your Anthropic API key and Spotify Client ID/Secret.
2. **Connect Spotify** — authorise the app via OAuth.
3. **Generate** — Claude scrapes the BBC page, then the app searches Spotify and
   (re)builds the playlist **"BBC 1xtra badman ting"** on your account.

Credentials are entered interactively and kept in memory only — nothing is
hardcoded and nothing is written to disk.

A resulting playlist looks like:
<https://open.spotify.com/playlist/4btEltH544et7aIypESPRO>

## Repeatable headless rebuild (`rebuild.py`)

The web app above makes you click through OAuth on every run. Once you've
authorised **once**, `rebuild.py` lets you rebuild the playlist any time with a
single command and **no interaction** — ideal for a cron job or a button.

It improves on the Go flow in two ways:

- **No client secret.** It uses the Authorization Code + **PKCE** refresh grant,
  so the (burned) secret in this repo's history is never needed.
- **Survives a blocked BBC fetch.** If the BBC page can't be fetched directly
  (e.g. a sandbox egress allowlist), it falls back to a reader proxy.

### One-time setup: get a refresh token

Run the PKCE authorization-code flow once (scopes `playlist-modify-public`,
`playlist-modify-private`, `user-read-private`) and keep the **refresh token**
it returns. Then:

```
cp .env.example .env      # fill in SPOTIFY_REFRESH_TOKEN and ANTHROPIC_API_KEY
python3 rebuild.py
```

`.env` is gitignored — credentials never get committed. If Spotify rotates the
refresh token, the script prints the new one to save.

### Flags

```
python3 rebuild.py                      # scrape + Claude extract + search + write
python3 rebuild.py --dry-run            # everything except writing the playlist
python3 rebuild.py --queries-file f.txt # skip scrape/extract; use a manual track list
python3 rebuild.py --name "My playlist" # override the playlist name
```

No third-party Python packages required (standard library only).

## Running in Claude Code on the web

This app talks to Spotify and the BBC, which are **not** on the default
"Trusted" network allowlist for cloud sessions. The allowlist is an environment
setting (configured in the web UI, not via a repo file): open the environment
for editing, set **Network access** to **Custom**, tick *"Also include default
list of common package managers"* (so Go modules and `api.anthropic.com` keep
working), and add these **Allowed domains**:

```text
accounts.spotify.com   # Spotify OAuth token exchange
api.spotify.com        # Spotify Web API (search, playlists)
www.bbc.co.uk          # the 1Xtra playlist page that gets scraped
```

Docs: <https://code.claude.com/docs/en/claude-code-on-the-web#network-access>

> Note: the OAuth flow redirects to `http://localhost:8080/callback`, so the
> interactive "Connect Spotify" step is meant to be run **locally**. In a cloud
> session the allowlist above lets the scrape (BBC + Claude) and Spotify Web API
> calls reach out, but the browser-based OAuth redirect won't resolve to the
> sandbox.
