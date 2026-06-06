# Session notes — for the next session

Branch: `claude/spotify-scraper-anthropic-3GTJU` (not merged, no PR open — left as-is).

## What this branch did

Rewrote the project from a brittle goquery/regex CLI into an **interactive web app
where Claude does the scraping**.

- `scraper.go` — fetches the BBC 1Xtra page, strips `<script>/<style>`, sends the
  HTML to `claude-opus-4-8` (official `anthropic-sdk-go`). Claude returns cleaned
  `Artist - Title` search queries (handles `feat`/`x`/`&`, `/` multi-song lines,
  `↑` arrows). Replaces both the old CSS-selector scrape and `BuildTrackQueries`.
- `spotify.go` — search + find-or-create-playlist, returns per-track found/missing.
- `main.go` — `net/http` server (`:8080`), 3-step UI: enter creds → OAuth → generate.
  Credentials are in-memory only. Hardcoded Spotify creds and `token.json` removed.
- README updated; added a "Running in Claude Code on the web" allowlist section.

Builds green: `go build ./...`, `go vet ./...`, `go mod tidy` all clean.
`anthropic-sdk-go v1.48.0` is a direct dep. `.gitignore` excludes the `/1xtra-spotify`
binary (a 20MB build artifact slipped into the first commit and was amended out).

## What is NOT yet verified (do this next)

Live end-to-end was **never run** — this sandbox's egress proxy blocked
`accounts.spotify.com`, `api.spotify.com`, `www.bbc.co.uk` (`Host not in allowlist`).
`api.anthropic.com` IS reachable on the default Trusted allowlist.

**Before testing in a cloud session:** the environment must use **Custom** network
access with these Allowed domains added (see README → "Running in Claude Code on the
web"): `accounts.spotify.com`, `api.spotify.com`, `www.bbc.co.uk`. Allowlist edits
only take effect in a **freshly started session** — the running container keeps the
policy it was provisioned with.

Test plan once unblocked:
1. Scrape path (needs BBC allowlisted + an Anthropic key): call `FetchPlaylistPage`
   then `ExtractTrackQueries` — confirm a sane list of `Artist - Title` queries.
2. Spotify search: `client.Search(...)` works with a token. NOTE the interactive
   OAuth flow redirects to `http://localhost:8080/callback`, which won't resolve to
   a cloud sandbox — that step is meant to run **locally**. For headless testing of
   search only, a client-credentials token works; playlist creation needs a user
   token (so test playlist creation locally).

## Loose ends / decisions for the user

- The old hardcoded Spotify creds (`72e5fca8…` / `7152279e…`) are committed in git
  history of the public repo — **rotate them**, they're burned.
- Dependabot reports 13 vulns (6 high, 7 moderate) on the default branch — mostly
  old transitive deps (logrus 1.5.0, zmb3/spotify pre-v2, etc.). Consider upgrading
  `zmb3/spotify` to v2 and `logrus` if revisiting.
- No PR opened (user asked to leave the branch). Open one when ready.
