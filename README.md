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
