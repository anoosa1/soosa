package subsonic

// SubsonicResponse is the top-level wrapper for all Subsonic API responses.
type SubsonicResponse struct {
	SubsonicResponse ResponseBody `json:"subsonic-response"`
}

type ResponseBody struct {
	Status        string         `json:"status"`
	Version       string         `json:"version"`
	Error         *APIError      `json:"error,omitempty"`
	SearchResult3 *SearchResult3 `json:"searchResult3,omitempty"`
	Song          *Song          `json:"song,omitempty"`
	Album         *Album         `json:"album,omitempty"`
	Artist        *ArtistFull    `json:"artist,omitempty"`
	RandomSongs   *RandomSongs   `json:"randomSongs,omitempty"`
}

type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type SearchResult3 struct {
	Artists []Artist `json:"artist,omitempty"`
	Albums  []Album  `json:"album,omitempty"`
	Songs   []Song   `json:"song,omitempty"`
}

type Song struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Album    string `json:"album,omitempty"`
	Artist   string `json:"artist,omitempty"`
	Duration int    `json:"duration,omitempty"` // seconds
	CoverArt string `json:"coverArt,omitempty"`
	Year     int    `json:"year,omitempty"`
	Genre    string `json:"genre,omitempty"`
	Track    int    `json:"track,omitempty"`
	BitRate  int    `json:"bitRate,omitempty"`
	Suffix   string `json:"suffix,omitempty"`
}

type Album struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Artist    string `json:"artist,omitempty"`
	ArtistID  string `json:"artistId,omitempty"`
	CoverArt  string `json:"coverArt,omitempty"`
	SongCount int    `json:"songCount,omitempty"`
	Duration  int    `json:"duration,omitempty"`
	Year      int    `json:"year,omitempty"`
	Genre     string `json:"genre,omitempty"`
	Songs     []Song `json:"song,omitempty"`
}

type Artist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AlbumCount int    `json:"albumCount,omitempty"`
	CoverArt   string `json:"coverArt,omitempty"`
}

type ArtistFull struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	AlbumCount int     `json:"albumCount,omitempty"`
	CoverArt   string  `json:"coverArt,omitempty"`
	Albums     []Album `json:"album,omitempty"`
}

type RandomSongs struct {
	Songs []Song `json:"song,omitempty"`
}
