#!/usr/bin/env python3
"""Headless, repeatable rebuild of the BBC 1Xtra Spotify playlist.

This is the non-interactive companion to the web app in `main.go`. The web app
makes you click through OAuth every run; this script does it once-and-done by
using a stored **refresh token**, so you can re-run it any time (cron, a button,
whatever) with zero interaction.

Security notes:
- Uses the **Authorization Code + PKCE** refresh grant, so **no client secret**
  is needed (the secret in this repo's git history is burned anyway).
- Reads all credentials from the environment / a gitignored `.env` — nothing
  sensitive is hardcoded or committed.

Pipeline (mirrors the Go app):
  1. refresh the Spotify access token        (no user interaction)
  2. fetch the BBC 1Xtra playlist page        (direct, falls back to a reader proxy)
  3. extract clean "Artist - Title" queries   (Claude, same prompt as scraper.go)
  4. search Spotify for each track            (with a simplified-query fallback)
  5. find-or-create the playlist & replace its tracks

Usage:
  export SPOTIFY_CLIENT_ID=...           # defaults to the app's public client id
  export SPOTIFY_REFRESH_TOKEN=...       # required (printed when you first authorize)
  export ANTHROPIC_API_KEY=sk-ant-...    # required unless --queries-file is given
  python3 rebuild.py
  python3 rebuild.py --queries-file tracks.txt   # skip scrape/extract, use a manual list
  python3 rebuild.py --dry-run                   # scrape + search, but don't write
"""

import argparse
import json
import os
import sys
import time
import urllib.parse
import urllib.request

PLAYLIST_PAGE = "https://www.bbc.co.uk/programmes/articles/2sgpCPqVPgjqC7tHBb97kd9/the-1xtra-playlist"
DEFAULT_PLAYLIST_NAME = "BBC 1xtra badman ting"
# The app's public client id. Public by design (it's in the OAuth redirect URL);
# override with SPOTIFY_CLIENT_ID if you point this at your own Spotify app.
DEFAULT_CLIENT_ID = "72e5fca8b6d34b3b912ddd620c2c2bd3"

EXTRACT_SYSTEM_PROMPT = (
    "You are a precise data-extraction tool. You are given the text of the BBC "
    "Radio 1Xtra playlist page. Find the list of tracks featured in the playlist "
    "and turn each one into a clean search query suitable for the Spotify search API.\n\n"
    "Rules for each query:\n"
    '- Format as "Artist - Title".\n'
    '- Strip any leading list markers, arrows (e.g. "↑"), or position indicators.\n'
    '- Normalise featured-artist noise: replace " featuring ", " feat ", " ft ", '
    '" ft. ", " x ", and " & " with a single space.\n'
    '- If one line lists several songs by the same artist separated by "/", emit '
    "one query per song, repeating the artist.\n"
    "- Ignore navigation links, related programmes, headings, social media, and "
    "anything that is not an actual track in the playlist.\n\n"
    'Respond with ONLY a JSON object of the form {"queries": ["Artist - Title", ...]} '
    "and nothing else — no prose, no code fences, no explanation."
)


def _post_form(url, fields, headers=None):
    data = urllib.parse.urlencode(fields).encode()
    req = urllib.request.Request(url, data=data, headers=headers or {}, method="POST")
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)


def _api(method, url, token, body=None):
    """Call the Spotify Web API, returning (status, parsed_json_or_text)."""
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            raw = r.read().decode() or "{}"
            return r.status, json.loads(raw)
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read().decode() or "{}")


# --- step 1: refresh the access token (PKCE grant, no secret) ---

def refresh_access_token(client_id, refresh_token):
    tok = _post_form(
        "https://accounts.spotify.com/api/token",
        {
            "grant_type": "refresh_token",
            "refresh_token": refresh_token,
            "client_id": client_id,
        },
        headers={"Content-Type": "application/x-www-form-urlencoded"},
    )
    if "access_token" not in tok:
        sys.exit(f"token refresh failed: {tok}")
    # Spotify may hand back a rotated refresh token; surface it so the user can save it.
    if tok.get("refresh_token") and tok["refresh_token"] != refresh_token:
        print("NOTE: Spotify rotated your refresh token. Save the new one:")
        print("  SPOTIFY_REFRESH_TOKEN=" + tok["refresh_token"])
    return tok["access_token"]


# --- step 2: fetch the playlist page (with reader-proxy fallback) ---

def fetch_playlist_page():
    ua = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
    try:
        req = urllib.request.Request(PLAYLIST_PAGE, headers={"User-Agent": ua})
        with urllib.request.urlopen(req, timeout=30) as r:
            if r.status == 200:
                return r.read().decode("utf-8", "replace")
    except Exception as e:
        print(f"direct BBC fetch failed ({e}); falling back to reader proxy")
    # r.jina.ai returns the page as clean reader-friendly text. Handy when the BBC
    # blocks the request or the host isn't on a sandbox egress allowlist.
    proxied = "https://r.jina.ai/" + PLAYLIST_PAGE
    req = urllib.request.Request(proxied, headers={"User-Agent": ua})
    with urllib.request.urlopen(req, timeout=60) as r:
        return r.read().decode("utf-8", "replace")


# --- step 3: extract Artist - Title queries with Claude ---

def extract_queries(page_text, anthropic_key):
    body = {
        "model": "claude-opus-4-8",
        "max_tokens": 8000,
        "system": EXTRACT_SYSTEM_PROMPT,
        "messages": [{"role": "user", "content": page_text}],
    }
    req = urllib.request.Request(
        "https://api.anthropic.com/v1/messages",
        data=json.dumps(body).encode(),
        headers={
            "x-api-key": anthropic_key,
            "anthropic-version": "2023-06-01",
            "content-type": "application/json",
        },
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=120) as r:
        resp = json.load(r)
    text = "".join(b.get("text", "") for b in resp.get("content", []))
    start, end = text.find("{"), text.rfind("}")
    if start == -1 or end == -1:
        sys.exit(f"Claude returned no JSON object: {text[:300]}")
    queries = json.loads(text[start : end + 1]).get("queries", [])
    if not queries:
        sys.exit("Claude found no tracks on the page")
    return queries


# --- step 4: search Spotify ---

def search_track(token, query):
    def _search(q):
        url = "https://api.spotify.com/v1/search?" + urllib.parse.urlencode(
            {"q": q, "type": "track", "limit": 1, "market": "GB"}
        )
        status, data = _api("GET", url, token)
        items = data.get("tracks", {}).get("items", []) if status == 200 else []
        return items[0] if items else None

    track = _search(query)
    if track:
        return track
    # Fall back to "<first artist> - <title>".
    if "-" in query:
        artist_part, _, title = query.partition("-")
        artists = artist_part.split()
        if artists and title.strip():
            return _search(f"{artists[0]} - {title.strip()}")
    return None


# --- step 5: find-or-create the playlist and replace its tracks ---

def find_or_create_playlist(token, user_id, name):
    url = "https://api.spotify.com/v1/me/playlists?limit=50"
    while url:
        status, data = _api("GET", url, token)
        if status != 200:
            sys.exit(f"listing playlists failed: {data}")
        for p in data.get("items", []):
            if p["name"] == name and p["owner"]["id"] == user_id:
                return p["id"]
        url = data.get("next")
    status, data = _api(
        "POST",
        f"https://api.spotify.com/v1/users/{user_id}/playlists",
        token,
        {"name": name, "public": True,
         "description": "Automatically scraped from the BBC website with Claude"},
    )
    if status not in (200, 201):
        sys.exit(f"creating playlist failed: {data}")
    return data["id"]


def replace_tracks(token, playlist_id, uris):
    # The replace endpoint takes up to 100 uris; first PUT replaces, the rest append.
    head, tail = uris[:100], uris[100:]
    status, data = _api(
        "PUT", f"https://api.spotify.com/v1/playlists/{playlist_id}/tracks", token,
        {"uris": head},
    )
    if status != 200:
        sys.exit(f"replacing tracks failed: {data}")
    for i in range(0, len(tail), 100):
        _api("POST", f"https://api.spotify.com/v1/playlists/{playlist_id}/tracks",
             token, {"uris": tail[i : i + 100]})


def load_dotenv(path=".env"):
    if not os.path.exists(path):
        return
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            k, _, v = line.partition("=")
            os.environ.setdefault(k.strip(), v.strip().strip('"').strip("'"))


def main():
    ap = argparse.ArgumentParser(description="Rebuild the BBC 1Xtra Spotify playlist.")
    ap.add_argument("--queries-file", help="skip scrape/extract; read Artist - Title lines from this file")
    ap.add_argument("--dry-run", action="store_true", help="search but don't modify the playlist")
    ap.add_argument("--name", default=os.environ.get("PLAYLIST_NAME", DEFAULT_PLAYLIST_NAME))
    args = ap.parse_args()

    load_dotenv()
    client_id = os.environ.get("SPOTIFY_CLIENT_ID", DEFAULT_CLIENT_ID)
    refresh_token = os.environ.get("SPOTIFY_REFRESH_TOKEN")
    if not refresh_token:
        sys.exit("SPOTIFY_REFRESH_TOKEN is required (authorize once, then save the refresh token).")

    print("1/5 refreshing Spotify access token (PKCE, no secret)...")
    token = refresh_access_token(client_id, refresh_token)

    if args.queries_file:
        with open(args.queries_file) as f:
            queries = [ln.strip() for ln in f if ln.strip()]
        print(f"using {len(queries)} queries from {args.queries_file}")
    else:
        anthropic_key = os.environ.get("ANTHROPIC_API_KEY")
        if not anthropic_key:
            sys.exit("ANTHROPIC_API_KEY is required (or pass --queries-file).")
        print("2/5 fetching the BBC 1Xtra playlist page...")
        page = fetch_playlist_page()
        print("3/5 asking Claude to extract the tracklist...")
        queries = extract_queries(page, anthropic_key)
        print(f"    Claude extracted {len(queries)} tracks")

    print("4/5 searching Spotify...")
    uris, found, missing = [], [], []
    for q in queries:
        track = search_track(token, q)
        if track:
            artists = ", ".join(a["name"] for a in track["artists"])
            print(f"    ✓ {q}  ->  {artists} - {track['name']}")
            uris.append(track["uri"])
            found.append(q)
        else:
            print(f"    ✗ {q}  (not found)")
            missing.append(q)
        time.sleep(0.05)

    print(f"    {len(found)}/{len(queries)} matched")
    if args.dry_run:
        print("dry run: not modifying the playlist.")
        return

    print("5/5 updating the playlist...")
    status, me = _api("GET", "https://api.spotify.com/v1/me", token)
    if status != 200:
        sys.exit(f"getting current user failed: {me}")
    playlist_id = find_or_create_playlist(token, me["id"], args.name)
    replace_tracks(token, playlist_id, uris)
    desc = "Automatically scraped from the BBC website with Claude - " + time.strftime(
        "%Y-%m-%dT%H:%M:%SZ", time.gmtime()
    )
    _api("PUT", f"https://api.spotify.com/v1/playlists/{playlist_id}", token, {"description": desc})

    status, pl = _api("GET",
        f"https://api.spotify.com/v1/playlists/{playlist_id}?fields=name,external_urls,tracks(total)",
        token)
    print(f"\nDone: \"{pl['name']}\" now has {pl['tracks']['total']} tracks")
    print(f"  {pl['external_urls']['spotify']}")
    if missing:
        print(f"  ({len(missing)} not found: {', '.join(missing)})")


if __name__ == "__main__":
    main()
