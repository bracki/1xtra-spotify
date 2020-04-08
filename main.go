package main

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/anaskhan96/soup"
	log "github.com/sirupsen/logrus"
	"github.com/zmb3/spotify"
)

const playlistURL = "https://www.bbc.co.uk/programmes/articles/2sgpCPqVPgjqC7tHBb97kd9/the-1xtra-playlist"
const redirectURI = "http://localhost:8080/callback"

var (
	auth  = spotify.NewAuthenticator(redirectURI, spotify.ScopeUserReadPrivate, spotify.ScopePlaylistModifyPrivate, spotify.ScopePlaylistModifyPublic)
	ch    = make(chan *spotify.Client)
	state = "abc123"
)

func CreateSpotifyClient() *spotify.Client{
	// first start an HTTP server
	http.HandleFunc("/callback", completeAuth)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Got request for:", r.URL.String())
	})
	go http.ListenAndServe(":8080", nil)

	url := auth.AuthURL(state)
	fmt.Println("Please log in to Spotify by visiting the following page in your browser:", url)

	// wait for auth to complete
	client := <-ch

	return client
}

func completeAuth(w http.ResponseWriter, r *http.Request) {
	tok, err := auth.Token(state, r)
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}
	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}
	// use the token to get an authenticated client
	client := auth.NewClient(tok)
	fmt.Fprintf(w, "Login Completed!")
	ch <- &client
}

// SearchTracksOnSpotifyAndCreatePlaylist finds tracks and adds them to playlist for the
// authorized user
func SearchTracksOnSpotifyAndCreatePlaylist(client *spotify.Client, trackQueries []string) error {
	// Find all tracks and collect them
	var fullTracks []spotify.FullTrack
	for _, query := range trackQueries {
		// Sanitize query by replacing strings that make Spotify unhappy
		query = strings.ReplaceAll(query, " featuring ", " ")
		query = strings.ReplaceAll(query, " ft ", " ")
		query = strings.ReplaceAll(query, " x ", " ")
		query = strings.ReplaceAll(query, " & ", " ")
		result, err := client.Search(query, spotify.SearchTypeTrack)
		if err != nil {
			log.Fatalf("couldn't find query: %v", err)
		}
		if result.Tracks.Total < 1 {
			log.WithField("query", query).Info("Couldn't find query")
		} else {
			fmt.Println(result.Tracks.Tracks[0])
			fullTracks = append(fullTracks, result.Tracks.Tracks[0])
		}
	}

	// Create a playlist featuring all the tracks for the current user
	user, err := client.CurrentUser()
	if err != nil {
		return err
	}
	playlistsForUser, err := client.GetPlaylistsForUser(user.ID)
	if err != nil {
		return err
	}
	playlistName := "BBC 1xtra badman ting"
	var playlistID spotify.ID
	for _, p := range playlistsForUser.Playlists {
		if p.Name == playlistName {
			playlistID = p.ID
		}
	}
	if playlistID == "" {
		playlist, err := client.CreatePlaylistForUser(user.ID, "BBC 1xtra badman ting", "Automatically scraped from the BBC website", true)
		if err != nil {
			return err
		}
		playlistID = playlist.ID
	}
	var trackIDs []spotify.ID
	for _, track := range fullTracks {
		trackIDs = append(trackIDs, track.ID)
	}
	date := time.Now().Format(time.RFC3339)
	err = client.ChangePlaylistDescription(playlistID, fmt.Sprintf("Automatically scraped from the BBC website - %v", date))
	if err != nil {
		return err
	}
	err = client.ReplacePlaylistTracks(playlistID, trackIDs...)
	if err != nil {
		return err
	}
	return nil
}

// ScrapeTracksFromPlaylist parses the tracks from the BBC 1xtra playlist website
func ScrapeTracksFromPlaylist() ([]string, error) {
	resp, err := soup.Get(playlistURL)

	if err != nil {
		return nil, err
	}
	doc := soup.HTMLParse(resp)
	prog := doc.Find("div", "class", "prog-layout")
	paragraphs := prog.FindAll("p")

	r, err := regexp.Compile(".* - .*")
	if err != nil {
		return nil, err
	}
	var tracks []string
	for _, p := range paragraphs {
		text := p.Text()
		text = strings.Replace(text,"↑ ", "", 1)
		if r.MatchString(text) {
			tracks = append(tracks, text)
		}
	}
	return tracks, nil
}

func main() {
	tracks, err := ScrapeTracksFromPlaylist()
	if err != nil {
		log.WithError(err).Fatal("Couldn't parse BBC playlist")
	}

	client := CreateSpotifyClient()
	_, err = client.CurrentUser()
	if err != nil {
		log.WithError(err).Fatal("Couldn't create Spotify client")
	}

	err = SearchTracksOnSpotifyAndCreatePlaylist(client, tracks)
	if err != nil {
		log.WithError(err).Fatal("Couldn't create Spotify playlist")
	}
}