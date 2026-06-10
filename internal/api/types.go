package api

// EasynewsSearchResponse mirrors the exact JSON shape from the Easynews API.
type EasynewsSearchResponse struct {
	SID               string           `json:"sid"`
	Results           int              `json:"results"`
	PerPage           string           `json:"perPage"`
	NumPages          int              `json:"numPages"`
	DlFarm            string           `json:"dlFarm"`
	DlPort            int              `json:"dlPort"`
	BaseURL           string           `json:"baseURL"`
	DownURL           string           `json:"downURL"`
	ThumbURL          string           `json:"thumbURL"`
	Page              int              `json:"page"`
	Groups            []map[string]int `json:"groups"`
	Data              []FileData       `json:"data"`
	Returned          int              `json:"returned"`
	UnfilteredResults int              `json:"unfilteredResults"`
	Hidden            int              `json:"hidden"`
	ClassicThumbs     string           `json:"classicThumbs"`
	Fields            Fields           `json:"fields"`
	Hthm              int              `json:"hthm"`
	HInfo             int              `json:"hInfo"`
	St                string           `json:"st"`
	SS                string           `json:"sS"`
	Stemmed           string           `json:"stemmed"`
	
	// Upgraded to interface{} to prevent runtime type mismatch crashes if Solr returns them as arrays or strings
	LargeThumb        interface{}      `json:"largeThumb"`
	LargeThumbSize    interface{}      `json:"largeThumbSize"`
	
	GsColumns         []GsColumn       `json:"gsColumns"`
}

// FileData represents a single file result. Numeric JSON keys map to string indexes.
type FileData struct {
	Zero          string      `json:"0"` // Hash ID
	One           string      `json:"1"` // Unknown
	Two           string      `json:"2"` // File extension
	Three         string      `json:"3"` // Unknown
	Four          string      `json:"4"` // Size string (e.g., "1.5 GB")
	Five          string      `json:"5"` // Date string
	Six           string      `json:"6"` // Unknown
	Seven         string      `json:"7"` // Unknown
	Eight         string      `json:"8"` // Unknown
	Nine          string      `json:"9"` // Group
	Ten           string      `json:"10"` // Post title / thumbnail slug
	Eleven        string      `json:"11"` // Extension (for path)
	Twelve        string      `json:"12"` // Compression standard
	Thirteen      string      `json:"13"` // Unknown
	Fourteen      string      `json:"14"` // Duration string
	Fifteen       int         `json:"15"` // Unknown numeric
	Sixteen       int         `json:"16"` // Unknown numeric
	Seventeen     int         `json:"17"` // Unknown numeric
	Eighteen      string      `json:"18"` // Coding format
	Nineteen      string      `json:"19"` // Unknown
	ThirtyFive    string      `json:"35"` // Unknown
	Type          string      `json:"type"` // Content type (e.g. VIDEO)
	Height        string      `json:"height"`
	Width         string      `json:"width"`
	Theight       int         `json:"theight"`
	Twidth        int         `json:"twidth"`
	Fullres       string      `json:"fullres"`
	Alangs        []string    `json:"alangs"`
	Slangs        interface{} `json:"slangs"`
	Passwd        bool        `json:"passwd"`
	Virus         bool        `json:"virus"`
	Expires       string      `json:"expires"`
	Nfo           string      `json:"nfo"`
	Ts            int64       `json:"ts"`      // Unix timestamp (seconds)
	RawSize       int64       `json:"rawSize"` // Size in bytes
	Volume        bool        `json:"volume"`
	Sc            bool        `json:"sc"`
	PrimaryURL    string      `json:"primaryURL"`
	FallbackURL   string      `json:"fallbackURL"`
	Sb            int         `json:"sb"`
	Size          int64       `json:"size"`
	Runtime       int         `json:"runtime"`
	Sig           string      `json:"sig"`
}

// ---------------------------------------------------------------------------
// Accessors (Value receivers for mathematical guarantee against nil-dereference)
// ---------------------------------------------------------------------------

func (f FileData) GetHash() string      { return f.Zero }
func (f FileData) GetExtension() string { return f.Two }
func (f FileData) GetSize() string      { return f.Four }
func (f FileData) GetDate() string      { return f.Five }
func (f FileData) GetDuration() string  { return f.Fourteen }
func (f FileData) GetPostTitle() string { return f.Ten }
func (f FileData) GetPathExt() string   { return f.Eleven }

// SearchOptions represents query settings for searching the API.
type SearchOptions struct {
	Query          string
	PageNr         int
	MaxResults     int
	Sort1          string
	Sort1Direction string
	Sort2          string
	Sort2Direction string
	Sort3          string
	Sort3Direction string
}

type Fields struct {
	Two       string `json:"2"`
	Three     string `json:"3"`
	Four      string `json:"4"`
	Five      string `json:"5"`
	Six       string `json:"6"`
	Seven     string `json:"7"`
	Nine      string `json:"9"`
	Ten       string `json:"10"`
	Twelve    string `json:"12"`
	Fourteen  string `json:"14"`
	Fifteen   string `json:"15"`
	Sixteen   string `json:"16"`
	Seventeen string `json:"17"`
	Eighteen  string `json:"18"`
	Twenty    string `json:"20"`
	FullThumb string `json:"FullThumb"`
}

type GsColumn struct {
	num  int    `json:"num"`
	name string `json:"name"`
}
