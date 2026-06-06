package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const playlistURL = "https://www.bbc.co.uk/programmes/articles/2sgpCPqVPgjqC7tHBb97kd9/the-1xtra-playlist"

// stripRe removes script/style/noscript blocks so we don't waste tokens (and
// confuse the model) on inline JavaScript and CSS.
var stripRe = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)
var blankLinesRe = regexp.MustCompile(`(?s)\n\s*\n+`)

// FetchPlaylistPage downloads the BBC 1xtra playlist page and returns its HTML
// with scripts and styles stripped out. We deliberately keep the markup (rather
// than extracting specific elements) so Claude can find the tracklist itself —
// that's what makes this resilient to the BBC changing their page layout.
func FetchPlaylistPage(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, playlistURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; 1xtra-spotify/1.0)")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching playlist page: status %d %s", res.StatusCode, res.Status)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, 5<<20))
	if err != nil {
		return "", err
	}

	html := stripRe.ReplaceAllString(string(body), " ")
	html = blankLinesRe.ReplaceAllString(html, "\n")
	return html, nil
}

const extractSystemPrompt = `You are a precise data-extraction tool. You are given the raw HTML of the ` +
	`BBC Radio 1Xtra playlist page. Find the list of tracks featured in the playlist and turn each one ` +
	`into a clean search query suitable for the Spotify search API.

Rules for each query:
- Format as "Artist - Title".
- Strip any leading list markers, arrows (e.g. "↑"), or position indicators.
- Normalise featured-artist noise: replace " featuring ", " feat ", " ft ", " ft. ", " x ", and " & " with a single space.
- If one line lists several songs by the same artist separated by "/", emit one query per song, repeating the artist.
- Ignore navigation links, related programmes, headings, social media, and anything that is not an actual track in the playlist.

Respond with ONLY a JSON object of the form {"queries": ["Artist - Title", ...]} and nothing else — no prose, no code fences, no explanation.`

type extraction struct {
	Queries []string `json:"queries"`
}

// ExtractTrackQueries asks Claude to read the page HTML and return a cleaned-up
// list of "Artist - Title" search queries. This replaces the old goquery/regex
// scraping AND the manual query-sanitising logic in one model call.
func ExtractTrackQueries(ctx context.Context, apiKey, pageHTML string) ([]string, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeOpus4_8,
		MaxTokens: 8000,
		System: []anthropic.TextBlockParam{
			{Text: extractSystemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(pageHTML)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("calling Claude: %w", err)
	}

	var sb strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}

	raw := extractJSONObject(sb.String())
	var ex extraction
	if err := json.Unmarshal([]byte(raw), &ex); err != nil {
		return nil, fmt.Errorf("parsing Claude response as JSON: %w (got: %s)", err, truncate(sb.String(), 300))
	}
	if len(ex.Queries) == 0 {
		return nil, fmt.Errorf("Claude found no tracks on the page")
	}
	return ex.Queries, nil
}

// extractJSONObject pulls the outermost {...} out of a model response, in case
// the model wraps it in stray text or code fences despite instructions.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end < start {
		return s
	}
	return s[start : end+1]
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
