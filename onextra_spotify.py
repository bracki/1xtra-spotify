"""Scrape the BBC 1Xtra playlist and print a Spotify link for every track.

The flow is intentionally split into small, side-effect-free pieces so the
interesting logic can be unit tested without touching the network:

    fetch_html        -> I/O   (BBC website)
    html_to_text      -> pure  (strip markup down to readable text)
    extract_tracks    -> Claude (robust artist/title extraction)
    spotify_query     -> pure  (build a precise Spotify search query)
    find_spotify_link -> I/O   (Spotify search API)

Compared with the original regex/goquery scraper, Claude does the messy job
of pulling clean artist/title pairs out of the page, so we no longer depend
on the exact HTML structure of the BBC article.
"""

from __future__ import annotations

import os
from dataclasses import dataclass

import requests
from bs4 import BeautifulSoup

PLAYLIST_URL = (
    "https://www.bbc.co.uk/programmes/articles/"
    "2sgpCPqVPgjqC7tHBb97kd9/the-1xtra-playlist"
)

# A fast, inexpensive model is plenty for structured extraction. Override with
# the CLAUDE_MODEL env var if you want to trade cost for capability.
DEFAULT_MODEL = os.environ.get("CLAUDE_MODEL", "claude-sonnet-4-6")

# Tool schema used to force Claude to return structured output rather than prose.
EXTRACTION_TOOL = {
    "name": "save_tracks",
    "description": "Save the list of music tracks extracted from the playlist page.",
    "input_schema": {
        "type": "object",
        "properties": {
            "tracks": {
                "type": "array",
                "items": {
                    "type": "object",
                    "properties": {
                        "artist": {
                            "type": "string",
                            "description": "Performing artist(s), e.g. 'Stormzy'",
                        },
                        "title": {
                            "type": "string",
                            "description": "Song title without the artist",
                        },
                    },
                    "required": ["artist", "title"],
                },
            }
        },
        "required": ["tracks"],
    },
}

EXTRACTION_PROMPT = (
    "The text below comes from a BBC 1Xtra playlist web page. "
    "Extract every music track listed on the page as a separate artist/title "
    "pair. A single line may contain multiple songs by the same artist "
    "(separated by '/') — split those into individual tracks. Ignore "
    "navigation, headings, programme blurb, and anything that is not a track. "
    "Do not invent tracks that are not present.\n\n"
    "Page text:\n{text}"
)


@dataclass(frozen=True)
class Track:
    artist: str
    title: str

    def __str__(self) -> str:  # pragma: no cover - trivial
        return f"{self.artist} - {self.title}"


def fetch_html(url: str = PLAYLIST_URL, *, session: requests.Session | None = None) -> str:
    """Download the raw HTML of the playlist page."""
    getter = session.get if session is not None else requests.get
    response = getter(url, timeout=30)
    response.raise_for_status()
    return response.text


def html_to_text(html: str) -> str:
    """Reduce a page to readable text, favouring the playlist container.

    We give Claude a hint by preferring the known ``.prog-layout`` container,
    but fall back to the whole document so the extraction still works if the
    BBC changes its markup.
    """
    soup = BeautifulSoup(html, "html.parser")
    container = soup.select_one(".prog-layout") or soup
    return container.get_text("\n", strip=True)


def extract_tracks(page_text: str, client, *, model: str = DEFAULT_MODEL) -> list[Track]:
    """Use Claude to pull a clean list of tracks out of the page text.

    ``client`` is an ``anthropic.Anthropic`` instance (injected so tests can
    pass a fake). Returns an empty list if nothing was extracted.
    """
    message = client.messages.create(
        model=model,
        max_tokens=4096,
        tools=[EXTRACTION_TOOL],
        tool_choice={"type": "tool", "name": "save_tracks"},
        messages=[
            {
                "role": "user",
                "content": EXTRACTION_PROMPT.format(text=page_text),
            }
        ],
    )

    for block in message.content:
        if getattr(block, "type", None) == "tool_use":
            raw_tracks = block.input.get("tracks", [])
            return [
                Track(artist=t["artist"].strip(), title=t["title"].strip())
                for t in raw_tracks
                if t.get("artist", "").strip() and t.get("title", "").strip()
            ]
    return []


def spotify_query(track: Track) -> str:
    """Build a precise Spotify search query using field filters."""
    return f'track:{track.title} artist:{track.artist}'


def find_spotify_link(sp, track: Track) -> str | None:
    """Search Spotify for a track and return its open.spotify.com URL.

    Tries a precise field-filtered query first, then falls back to a looser
    free-text query before giving up. ``sp`` is a ``spotipy.Spotify`` client.
    """
    for query in (spotify_query(track), f"{track.artist} {track.title}"):
        results = sp.search(q=query, type="track", limit=1)
        items = results.get("tracks", {}).get("items", [])
        if items:
            return items[0]["external_urls"]["spotify"]
    return None


def make_spotify_client(client_id: str, client_secret: str):
    """Create a search-only Spotify client (Client Credentials flow)."""
    import spotipy
    from spotipy.oauth2 import SpotifyClientCredentials

    auth = SpotifyClientCredentials(client_id=client_id, client_secret=client_secret)
    return spotipy.Spotify(client_credentials_manager=auth)


def main() -> None:
    import anthropic

    spotify_id = os.environ.get("SPOTIFY_ID")
    spotify_secret = os.environ.get("SPOTIFY_SECRET")
    if not spotify_id or not spotify_secret:
        raise SystemExit("Set SPOTIFY_ID and SPOTIFY_SECRET environment variables.")
    if not os.environ.get("ANTHROPIC_API_KEY"):
        raise SystemExit("Set the ANTHROPIC_API_KEY environment variable.")

    html = fetch_html()
    text = html_to_text(html)

    claude = anthropic.Anthropic()
    tracks = extract_tracks(text, claude)
    if not tracks:
        raise SystemExit("Claude did not extract any tracks from the page.")

    sp = make_spotify_client(spotify_id, spotify_secret)
    for track in tracks:
        link = find_spotify_link(sp, track)
        print(f"{track}: {link or '(not found on Spotify)'}")


if __name__ == "__main__":
    main()
