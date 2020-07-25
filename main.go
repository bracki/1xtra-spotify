package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	log "github.com/sirupsen/logrus"
	"github.com/zmb3/spotify"
	"golang.org/x/oauth2"
)

const playlistURL = "https://www.bbc.co.uk/programmes/articles/2sgpCPqVPgjqC7tHBb97kd9/the-1xtra-playlist"
const redirectURI = "http://localhost:8080/callback"

var (
	auth  = spotify.NewAuthenticator(redirectURI, spotify.ScopeUserReadPrivate, spotify.ScopePlaylistModifyPrivate, spotify.ScopePlaylistModifyPublic)
	ch    = make(chan *spotify.Client)
	state = "abc123"
)

func CreateSpotifyClient() *spotify.Client {
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
	data, err := json.Marshal(tok)
	if err != nil {
		log.Fatal("Can't save token to disk")
	}
	log.WithField("token", string(data)).Info("Here's the token...")
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

func CreateSpotifyClientFromSavedToken() *spotify.Client {
	auth.SetAuthInfo("72e5fca8b6d34b3b912ddd620c2c2bd3", "7152279e600a4e2892685dda6eb55b65")
	data, err := ioutil.ReadFile("token.json")
	if err != nil {
		log.WithError(err).Fatal("Couldn't load token file")
	}

	var tok oauth2.Token
	err = json.Unmarshal(data, &tok)
	if err != nil {
		log.WithError(err).Fatal("Couldn't unmarshal token")
	}

	client := auth.NewClient(&tok)

	return &client
}

func BuildTrackQueries(tracks []string) []string {
	var queries []string
	for _, track := range tracks {
		// Sanitize query by replacing strings that make Spotify unhappy
		track = strings.ReplaceAll(track, " featuring ", " ")
		track = strings.ReplaceAll(track, " ft ", " ")
		track = strings.ReplaceAll(track, " x ", " ")
		track = strings.ReplaceAll(track, " & ", " ")
		// Multiple songs in one line
		if strings.Contains(track, "/") {
			// Split query into artist and song
			s := strings.Split(track, "-")
			artist := s[0]
			songs := strings.Split(s[1], "/")
			for _, s := range songs {
				queries = append(queries, fmt.Sprintf("%s - %s", artist, s))
			}
		} else {
			queries = append(queries, track)
		}
	}
	return queries
}

// SearchTracksOnSpotifyAndCreatePlaylist finds tracks and adds them to playlist for the
// authorized user
func SearchTracksOnSpotifyAndCreatePlaylist(client *spotify.Client, trackQueries []string) error {
	// Find all tracks and collect them
	var fullTracks []spotify.FullTrack
	for _, query := range trackQueries {
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
	// Request the HTML page.
	res, err := http.Get(playlistURL)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		log.Fatalf("status code error: %d %s", res.StatusCode, res.Status)
	}

	// Load the HTML document
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Fatal(err)
	}

	// Find the tracks
	r, err := regexp.Compile(".* - .*")
	if err != nil {
		return nil, err
	}
	var tracks []string
	doc.Find(".prog-layout .text--prose").Each(func(i int, s *goquery.Selection) {
		// For each item found, scrape the track
		s.Find("p").Contents().Each(func(i int, s *goquery.Selection) {
			if !s.Is("br") {
				text := strings.Replace(s.Text(), "↑ ", "", 1)
				if r.MatchString(text) {
					tracks = append(tracks, text)
				}
			}
		})
	})
	return tracks, nil
}

func main() {
	tracks, err := ScrapeTracksFromPlaylist()
	if err != nil {
		log.WithError(err).Fatal("doof")
	}
	tracks = BuildTrackQueries(tracks)
	if err != nil {
		log.WithError(err).Fatal("Couldn't parse BBC playlist")
	}

	client := CreateSpotifyClientFromSavedToken()
	u, err := client.CurrentUser()
	if err != nil {
		log.WithError(err).Fatal("Couldn't create Spotify client")
	}
	log.WithField("user", fmt.Sprintf("%v", u)).Info("Authenticated")

	err = SearchTracksOnSpotifyAndCreatePlaylist(client, tracks)
	if err != nil {
		log.WithError(err).Fatal("Couldn't create Spotify playlist")
	}
}
