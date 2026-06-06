# BBC 1Xtra playlist generator for Spotify

Scrapes the [BBC 1Xtra playlist](https://www.bbc.co.uk/programmes/articles/2sgpCPqVPgjqC7tHBb97kd9/the-1xtra-playlist)
and prints a Spotify link for every track.

## Python version (recommended)

Instead of brittle CSS/regex parsing, this version asks **Claude** to extract
clean `artist` / `title` pairs from the page, then uses the **Spotify search
API** (Client Credentials flow — no interactive login) to resolve each track to
its `open.spotify.com` URL.

### Setup

```
python3 -m venv .venv
. .venv/bin/activate
pip install -r requirements.txt
```

### Run

```
export ANTHROPIC_API_KEY=...
export SPOTIFY_ID=...
export SPOTIFY_SECRET=...
python onextra_spotify.py
```

This prints lines like:

```
Stormzy - Vossi Bop: https://open.spotify.com/track/1HCMUmKbgvFc8E2HFwzRBz
```

Optional: set `CLAUDE_MODEL` to override the extraction model (defaults to
`claude-sonnet-4-6`).

### Tests

The logic is split so everything except the two network boundaries
(`fetch_html` and the live Spotify/Claude calls) is unit tested with fakes:

```
pip install pytest
python -m pytest
```

## Legacy Go version

The original Go implementation (`main.go`) creates a private playlist via the
Spotify OAuth user flow. It is kept for reference.

```
export SPOTIFY_ID=...
export SPOTIFY_SECRET=...
go run main.go
```
