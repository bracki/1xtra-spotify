package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/zmb3/spotify"
)

// TrackResult records what happened to a single search query.
type TrackResult struct {
	Query  string
	Found  bool
	Name   string
	Artist string
	URL    string
}

// GenerateResult is the outcome of a full scrape → search → playlist run.
type GenerateResult struct {
	PlaylistName string
	PlaylistURL  string
	Tracks       []TrackResult
	FoundCount   int
}

const playlistName = "BBC 1xtra badman ting"

// searchTrack looks up a single query on Spotify, falling back to a simplified
// query (first artist + title) when the full query returns nothing.
func searchTrack(client *spotify.Client, query string) (*spotify.FullTrack, error) {
	result, err := client.Search(query, spotify.SearchTypeTrack)
	if err != nil {
		return nil, err
	}
	if result.Tracks != nil && result.Tracks.Total > 0 {
		return &result.Tracks.Tracks[0], nil
	}

	// Fall back to "<first artist> - <title>".
	if parts := strings.SplitN(query, "-", 2); len(parts) == 2 {
		artists := strings.Fields(strings.TrimSpace(parts[0]))
		title := strings.TrimSpace(parts[1])
		if len(artists) > 0 && title != "" {
			simplified := fmt.Sprintf("%s - %s", artists[0], title)
			result, err = client.Search(simplified, spotify.SearchTypeTrack)
			if err != nil {
				return nil, err
			}
			if result.Tracks != nil && result.Tracks.Total > 0 {
				return &result.Tracks.Tracks[0], nil
			}
		}
	}
	return nil, nil
}

// BuildPlaylist searches Spotify for every query, then creates (or reuses) the
// playlist for the authenticated user and replaces its contents with the found
// tracks.
func BuildPlaylist(client *spotify.Client, queries []string) (*GenerateResult, error) {
	res := &GenerateResult{PlaylistName: playlistName}

	var trackIDs []spotify.ID
	for _, query := range queries {
		tr := TrackResult{Query: query}
		track, err := searchTrack(client, query)
		if err != nil {
			return nil, fmt.Errorf("searching for %q: %w", query, err)
		}
		if track != nil {
			tr.Found = true
			tr.Name = track.Name
			tr.URL = track.ExternalURLs["spotify"]
			if len(track.Artists) > 0 {
				tr.Artist = track.Artists[0].Name
			}
			trackIDs = append(trackIDs, track.ID)
			res.FoundCount++
		}
		res.Tracks = append(res.Tracks, tr)
	}

	user, err := client.CurrentUser()
	if err != nil {
		return nil, fmt.Errorf("getting current user: %w", err)
	}

	playlistID, playlistURL, err := findOrCreatePlaylist(client, user.ID)
	if err != nil {
		return nil, err
	}
	res.PlaylistURL = playlistURL

	desc := fmt.Sprintf("Automatically scraped from the BBC website with Claude - %s",
		time.Now().Format(time.RFC3339))
	if err := client.ChangePlaylistDescription(playlistID, desc); err != nil {
		return nil, fmt.Errorf("updating playlist description: %w", err)
	}
	if err := client.ReplacePlaylistTracks(playlistID, trackIDs...); err != nil {
		return nil, fmt.Errorf("replacing playlist tracks: %w", err)
	}
	return res, nil
}

func findOrCreatePlaylist(client *spotify.Client, userID string) (spotify.ID, string, error) {
	page, err := client.GetPlaylistsForUser(userID)
	if err != nil {
		return "", "", fmt.Errorf("listing playlists: %w", err)
	}
	for _, p := range page.Playlists {
		if p.Name == playlistName {
			return p.ID, p.ExternalURLs["spotify"], nil
		}
	}

	playlist, err := client.CreatePlaylistForUser(userID, playlistName,
		"Automatically scraped from the BBC website with Claude", true)
	if err != nil {
		return "", "", fmt.Errorf("creating playlist: %w", err)
	}
	return playlist.ID, playlist.ExternalURLs["spotify"], nil
}
