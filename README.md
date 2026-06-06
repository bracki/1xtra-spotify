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
