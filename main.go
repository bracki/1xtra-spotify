package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/zmb3/spotify"
)

const (
	listenAddr  = ":8080"
	redirectURI = "http://localhost:8080/callback"
)

// app holds the (single-user) runtime state for the tool: the credentials the
// user typed into the website and the resulting Spotify client.
type app struct {
	mu sync.Mutex

	anthropicKey  string
	spotifyID     string
	spotifySecret string

	auth   spotify.Authenticator
	state  string
	client *spotify.Client
	user   string

	lastResult *GenerateResult
	lastError  string
}

var a = &app{}

func main() {
	// Load a .env file (if present) into the environment before reading creds.
	loadDotEnv(".env")

	// Pre-fill credentials from the environment if present, so you can skip the
	// web form. Any/all of these are optional; missing ones fall back to the UI.
	a.applyConfig(
		os.Getenv("ANTHROPIC_API_KEY"),
		os.Getenv("SPOTIFY_ID"),
		os.Getenv("SPOTIFY_SECRET"),
	)
	if a.configured() {
		log.Info("Credentials loaded from environment — go straight to 'Connect Spotify'")
	}

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/config", handleConfig)
	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/callback", handleCallback)
	http.HandleFunc("/generate", handleGenerate)

	log.WithField("url", "http://localhost"+listenAddr).Info("1xtra-spotify is running — open this in your browser")
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		log.WithError(err).Fatal("server stopped")
	}
}

// configured reports whether all three credentials have been supplied.
func (a *app) configured() bool {
	return a.anthropicKey != "" && a.spotifyID != "" && a.spotifySecret != ""
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	render(w)
}

// handleConfig stores the credentials the user submitted and prepares the
// Spotify authenticator.
func handleConfig(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.applyConfig(
		r.FormValue("anthropic_key"),
		r.FormValue("spotify_id"),
		r.FormValue("spotify_secret"),
	)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// applyConfig records the credentials and, once all are present, prepares the
// Spotify authenticator. Shared by the web form and the env-var bootstrap.
// Caller must hold a.mu (or be running before the server starts).
func (a *app) applyConfig(anthropicKey, spotifyID, spotifySecret string) {
	a.anthropicKey = anthropicKey
	a.spotifyID = spotifyID
	a.spotifySecret = spotifySecret

	// Reset any previous Spotify session — the credentials may have changed.
	a.client = nil
	a.user = ""

	if a.configured() {
		auth := spotify.NewAuthenticator(redirectURI,
			spotify.ScopeUserReadPrivate,
			spotify.ScopePlaylistModifyPrivate,
			spotify.ScopePlaylistModifyPublic,
		)
		auth.SetAuthInfo(a.spotifyID, a.spotifySecret)
		a.auth = auth
		a.state = randomState()
	}
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.configured() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, a.auth.AuthURL(a.state), http.StatusSeeOther)
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.configured() {
		http.Error(w, "credentials not configured", http.StatusBadRequest)
		return
	}

	tok, err := a.auth.Token(a.state, r)
	if err != nil {
		log.WithError(err).Error("couldn't get Spotify token")
		http.Error(w, "couldn't get Spotify token: "+err.Error(), http.StatusForbidden)
		return
	}

	client := a.auth.NewClient(tok)
	a.client = &client

	if u, err := client.CurrentUser(); err == nil {
		a.user = u.DisplayName
		if a.user == "" {
			a.user = u.ID
		}
	}
	log.WithField("user", a.user).Info("Spotify connected")

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleGenerate runs the whole pipeline: fetch the BBC page, have Claude
// extract the tracklist, then search Spotify and (re)build the playlist.
func handleGenerate(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.lastResult = nil
	a.lastError = ""

	if a.client == nil {
		a.lastError = "Connect Spotify before generating a playlist."
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	pageHTML, err := FetchPlaylistPage(ctx)
	if err != nil {
		a.lastError = "Couldn't fetch the BBC playlist page: " + err.Error()
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	queries, err := ExtractTrackQueries(ctx, a.anthropicKey, pageHTML)
	if err != nil {
		a.lastError = "Claude couldn't extract the tracklist: " + err.Error()
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	log.WithField("count", len(queries)).Info("Claude extracted tracks")

	result, err := BuildPlaylist(a.client, queries)
	if err != nil {
		a.lastError = "Couldn't build the Spotify playlist: " + err.Error()
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.lastResult = result

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// loadDotEnv reads KEY=VALUE pairs from a .env file (if present) into the
// process environment. Real environment variables take precedence, so you can
// still override the file on the command line. A missing file is not an error.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}

func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- view ---

type viewData struct {
	Configured       bool
	SpotifyConnected bool
	SpotifyUser      string
	Result           *GenerateResult
	Error            string
}

func render(w http.ResponseWriter) {
	data := viewData{
		Configured:       a.configured(),
		SpotifyConnected: a.client != nil,
		SpotifyUser:      a.user,
		Result:           a.lastResult,
		Error:            a.lastError,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.Execute(w, data); err != nil {
		log.WithError(err).Error("rendering page")
	}
}

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>BBC 1Xtra → Spotify (powered by Claude)</title>
<style>
  :root { color-scheme: light dark; }
  body { font-family: system-ui, sans-serif; max-width: 760px; margin: 2rem auto; padding: 0 1rem; line-height: 1.5; }
  h1 { font-size: 1.6rem; }
  .step { border: 1px solid #8884; border-radius: 10px; padding: 1rem 1.25rem; margin: 1rem 0; }
  .done { border-color: #2ecc7088; }
  label { display: block; font-weight: 600; margin-top: .75rem; }
  input[type=text], input[type=password] { width: 100%; padding: .5rem; margin-top: .25rem; box-sizing: border-box; border-radius: 6px; border: 1px solid #8888; }
  button { margin-top: 1rem; padding: .6rem 1.1rem; font-size: 1rem; border-radius: 8px; border: 0; background: #1db954; color: #fff; cursor: pointer; }
  button:disabled { background: #8884; cursor: not-allowed; }
  .muted { color: #8a8a8a; font-size: .9rem; }
  .badge { font-size: .8rem; padding: .1rem .5rem; border-radius: 999px; background: #2ecc7033; color: #1a9e5a; }
  table { border-collapse: collapse; width: 100%; margin-top: 1rem; }
  th, td { text-align: left; padding: .4rem .5rem; border-bottom: 1px solid #8883; font-size: .92rem; }
  .miss { color: #d33; }
  .err { background: #ff525233; border: 1px solid #d33; padding: .75rem 1rem; border-radius: 8px; }
</style>
</head>
<body>
  <h1>BBC 1Xtra → Spotify 🎧</h1>
  <p class="muted">Scrapes the BBC 1Xtra playlist with <strong>Claude</strong> and rebuilds it as a Spotify playlist. Your credentials stay in memory on this machine and are never written to disk.</p>

  {{if .Error}}<div class="err">⚠️ {{.Error}}</div>{{end}}

  <div class="step {{if .Configured}}done{{end}}">
    <h2>1. Credentials {{if .Configured}}<span class="badge">configured</span>{{end}}</h2>
    <p class="muted">Spotify app credentials come from your <a href="https://developer.spotify.com/dashboard" target="_blank" rel="noopener">Spotify developer dashboard</a>. Add <code>{{/* */}}http://localhost:8080/callback</code> as a Redirect URI there. The Anthropic key is used for the scraping step.</p>
    <form method="post" action="/config">
      <label>Anthropic API key</label>
      <input type="password" name="anthropic_key" placeholder="sk-ant-..." autocomplete="off">
      <label>Spotify Client ID</label>
      <input type="text" name="spotify_id" autocomplete="off">
      <label>Spotify Client Secret</label>
      <input type="password" name="spotify_secret" autocomplete="off">
      <button type="submit">Save credentials</button>
    </form>
  </div>

  <div class="step {{if .SpotifyConnected}}done{{end}}">
    <h2>2. Connect Spotify {{if .SpotifyConnected}}<span class="badge">connected{{if .SpotifyUser}} as {{.SpotifyUser}}{{end}}</span>{{end}}</h2>
    <form method="get" action="/login">
      <button type="submit" {{if not .Configured}}disabled{{end}}>
        {{if .SpotifyConnected}}Reconnect Spotify{{else}}Connect Spotify{{end}}
      </button>
    </form>
    {{if not .Configured}}<p class="muted">Save your credentials first.</p>{{end}}
  </div>

  <div class="step">
    <h2>3. Generate the playlist</h2>
    <form method="post" action="/generate">
      <button type="submit" {{if not .SpotifyConnected}}disabled{{end}}>Scrape with Claude &amp; build playlist</button>
    </form>
    {{if not .SpotifyConnected}}<p class="muted">Connect Spotify first.</p>{{end}}
  </div>

  {{with .Result}}
  <div class="step done">
    <h2>Result</h2>
    <p>Added <strong>{{.FoundCount}}</strong> of {{len .Tracks}} tracks to
      {{if .PlaylistURL}}<a href="{{.PlaylistURL}}" target="_blank" rel="noopener">{{.PlaylistName}}</a>{{else}}{{.PlaylistName}}{{end}}.</p>
    <table>
      <thead><tr><th>Query</th><th>Match</th></tr></thead>
      <tbody>
      {{range .Tracks}}
        <tr>
          <td>{{.Query}}</td>
          {{if .Found}}<td>{{.Artist}} — {{.Name}}{{if .URL}} (<a href="{{.URL}}" target="_blank" rel="noopener">open</a>){{end}}</td>
          {{else}}<td class="miss">not found</td>{{end}}
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>
  {{end}}
</body>
</html>`))
