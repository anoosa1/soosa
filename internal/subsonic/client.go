package subsonic

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

const (
	apiVersion = "1.16.1"
	clientName = "soosa"
)

// Client wraps the Subsonic REST API.
type Client struct {
	BaseURL    string
	User       string
	Password   string
	HTTP       *http.Client // For API calls (short timeout)
	StreamHTTP *http.Client // For downloading audio (no timeout)
}

// NewClient creates a new Subsonic API client.
func NewClient(baseURL, user, password string) *Client {
	return &Client{
		BaseURL:  baseURL,
		User:     user,
		Password: password,
		HTTP: &http.Client{
			Timeout: 10 * time.Second,
		},
		StreamHTTP: &http.Client{},
	}
}

// authParams returns the common authentication query parameters.
func (c *Client) authParams() url.Values {
	salt := randomSalt(12)
	token := md5Hash(c.Password + salt)

	params := url.Values{}
	params.Set("u", c.User)
	params.Set("t", token)
	params.Set("s", salt)
	params.Set("v", apiVersion)
	params.Set("c", clientName)
	params.Set("f", "json")
	return params
}

// buildURL constructs a full API URL for the given endpoint and extra params.
func (c *Client) buildURL(endpoint string, extra url.Values) string {
	params := c.authParams()
	for k, vs := range extra {
		for _, v := range vs {
			params.Set(k, v)
		}
	}
	return fmt.Sprintf("%s/rest/%s?%s", c.BaseURL, endpoint, params.Encode())
}

// get performs a GET request and decodes the JSON response.
func (c *Client) get(endpoint string, extra url.Values) (*ResponseBody, error) {
	reqURL := c.buildURL(endpoint, extra)

	resp, err := c.HTTP.Get(reqURL)
	if err != nil {
		log.Printf("[SUBSONIC ERROR] Request failed: %s | Error: %v", endpoint, err)
		return nil, fmt.Errorf("subsonic request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var sr SubsonicResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if sr.SubsonicResponse.Status != "ok" {
		if sr.SubsonicResponse.Error != nil {
			log.Printf("[SUBSONIC API ERROR] Method: %s | Code: %d | Message: %s", endpoint, sr.SubsonicResponse.Error.Code, sr.SubsonicResponse.Error.Message)
			return nil, fmt.Errorf("subsonic error %d: %s",
				sr.SubsonicResponse.Error.Code,
				sr.SubsonicResponse.Error.Message)
		}
		return nil, fmt.Errorf("subsonic returned status: %s", sr.SubsonicResponse.Status)
	}

	return &sr.SubsonicResponse, nil
}

// Ping verifies connectivity to the Subsonic server.
func (c *Client) Ping() error {
	_, err := c.get("ping.view", nil)
	return err
}

// Search performs a search3 query returning songs, albums, and artists.
func (c *Client) Search(query string, count int) (*SearchResult3, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("songCount", fmt.Sprintf("%d", count))
	params.Set("albumCount", fmt.Sprintf("%d", count))
	params.Set("artistCount", fmt.Sprintf("%d", count))

	resp, err := c.get("search3.view", params)
	if err != nil {
		return nil, err
	}
	if resp.SearchResult3 == nil {
		return &SearchResult3{}, nil
	}
	return resp.SearchResult3, nil
}

// GetSong returns metadata for a specific song.
func (c *Client) GetSong(id string) (*Song, error) {
	params := url.Values{}
	params.Set("id", id)

	resp, err := c.get("getSong.view", params)
	if err != nil {
		return nil, err
	}
	return resp.Song, nil
}

// GetAlbum returns an album with its tracks.
func (c *Client) GetAlbum(id string) (*Album, error) {
	params := url.Values{}
	params.Set("id", id)

	resp, err := c.get("getAlbum.view", params)
	if err != nil {
		return nil, err
	}
	return resp.Album, nil
}

// GetArtist returns an artist with their albums.
func (c *Client) GetArtist(id string) (*ArtistFull, error) {
	params := url.Values{}
	params.Set("id", id)

	resp, err := c.get("getArtist.view", params)
	if err != nil {
		return nil, err
	}
	return resp.Artist, nil
}

// GetRandomSongs returns random songs from the library.
func (c *Client) GetRandomSongs(count int) ([]Song, error) {
	params := url.Values{}
	params.Set("size", fmt.Sprintf("%d", count))

	resp, err := c.get("getRandomSongs.view", params)
	if err != nil {
		return nil, err
	}
	if resp.RandomSongs == nil {
		return []Song{}, nil
	}
	return resp.RandomSongs.Songs, nil
}

// StreamURL returns the authenticated URL for streaming a song. This URL is NOT fetched by the client — it's passed to ffmpeg/dca.

func (c *Client) StreamURL(id string) string {
	params := url.Values{}
	params.Set("id", id)
	return c.buildURL("stream.view", params)
}

// GetCoverArtURL returns the authenticated URL for cover art.
func (c *Client) GetCoverArtURL(id string, size int) string {
	params := url.Values{}
	params.Set("id", id)
	if size > 0 {
		params.Set("size", fmt.Sprintf("%d", size))
	}
	return c.buildURL("getCoverArt.view", params)
}

// --- Helpers ---

func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

func randomSalt(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// In the extremely rare case of error, we just fallback or return empty (though on Linux this shouldn't happen unless /dev/urandom is broken)

	}
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}
