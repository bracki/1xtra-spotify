"""Unit tests for the BBC 1Xtra -> Spotify scraper.

These cover everything except the two real network boundaries (`fetch_html`
and the actual Spotify/Claude HTTP calls), which are exercised via fakes.
"""

import onextra_spotify as ox
from onextra_spotify import Track


# --- html_to_text -----------------------------------------------------------


def test_html_to_text_prefers_playlist_container():
    html = """
    <html><body>
        <nav>menu junk</nav>
        <div class="prog-layout"><p>Stormzy - Vossi Bop</p></div>
    </body></html>
    """
    text = ox.html_to_text(html)
    assert "Stormzy - Vossi Bop" in text
    assert "menu junk" not in text


def test_html_to_text_falls_back_to_whole_document():
    html = "<html><body><p>Dave - Titanium</p></body></html>"
    text = ox.html_to_text(html)
    assert "Dave - Titanium" in text


# --- spotify_query -----------------------------------------------------------


def test_spotify_query_uses_field_filters():
    q = ox.spotify_query(Track(artist="Stormzy", title="Vossi Bop"))
    assert q == "track:Vossi Bop artist:Stormzy"


# --- extract_tracks (Claude injected as a fake) ------------------------------


class _Block:
    def __init__(self, type_, input_=None):
        self.type = type_
        self.input = input_


class _Message:
    def __init__(self, content):
        self.content = content


class _FakeClaude:
    """Minimal stand-in for anthropic.Anthropic that records the call."""

    def __init__(self, tracks):
        self._tracks = tracks
        self.last_kwargs = None
        self.messages = self

    def create(self, **kwargs):
        self.last_kwargs = kwargs
        return _Message([_Block("tool_use", {"tracks": self._tracks})])


def test_extract_tracks_parses_tool_output():
    client = _FakeClaude(
        [
            {"artist": "Stormzy", "title": "Vossi Bop"},
            {"artist": "Dave", "title": "Titanium"},
        ]
    )
    tracks = ox.extract_tracks("some page text", client)
    assert tracks == [
        Track("Stormzy", "Vossi Bop"),
        Track("Dave", "Titanium"),
    ]


def test_extract_tracks_forces_the_tool_and_passes_text():
    client = _FakeClaude([])
    ox.extract_tracks("PAGE BODY", client, model="claude-sonnet-4-6")
    kwargs = client.last_kwargs
    assert kwargs["tool_choice"] == {"type": "tool", "name": "save_tracks"}
    assert kwargs["model"] == "claude-sonnet-4-6"
    assert "PAGE BODY" in kwargs["messages"][0]["content"]


def test_extract_tracks_skips_blank_entries_and_trims():
    client = _FakeClaude(
        [
            {"artist": "  Stormzy  ", "title": "  Vossi Bop  "},
            {"artist": "", "title": "No Artist"},
            {"artist": "No Title", "title": "   "},
        ]
    )
    assert ox.extract_tracks("x", client) == [Track("Stormzy", "Vossi Bop")]


def test_extract_tracks_returns_empty_without_tool_use():
    class NoToolClaude:
        def __init__(self):
            self.messages = self

        def create(self, **kwargs):
            return _Message([_Block("text")])

    assert ox.extract_tracks("x", NoToolClaude()) == []


# --- find_spotify_link (Spotify injected as a fake) --------------------------


class _FakeSpotify:
    """Returns canned search results keyed by query, recording all queries."""

    def __init__(self, responses):
        self._responses = responses
        self.queries = []

    def search(self, q, type, limit):
        self.queries.append(q)
        items = self._responses.get(q, [])
        return {"tracks": {"items": items}}


def _item(url):
    return {"external_urls": {"spotify": url}}


def test_find_spotify_link_uses_precise_query_first():
    track = Track("Stormzy", "Vossi Bop")
    sp = _FakeSpotify({ox.spotify_query(track): [_item("https://open.spotify.com/track/abc")]})
    assert ox.find_spotify_link(sp, track) == "https://open.spotify.com/track/abc"
    # Only the precise query should have been tried.
    assert sp.queries == [ox.spotify_query(track)]


def test_find_spotify_link_falls_back_to_loose_query():
    track = Track("Stormzy", "Vossi Bop")
    sp = _FakeSpotify({"Stormzy Vossi Bop": [_item("https://open.spotify.com/track/xyz")]})
    assert ox.find_spotify_link(sp, track) == "https://open.spotify.com/track/xyz"
    assert sp.queries == [ox.spotify_query(track), "Stormzy Vossi Bop"]


def test_find_spotify_link_returns_none_when_nothing_found():
    track = Track("Nobody", "Nothing")
    sp = _FakeSpotify({})
    assert ox.find_spotify_link(sp, track) is None
    assert len(sp.queries) == 2  # tried both queries before giving up
