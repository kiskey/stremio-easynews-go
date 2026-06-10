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
	
	// Safely defined as interface{} to prevent decoding failures on different query formats
	LargeThumb        interface{}      `json:"largeThumb"`
	LargeThumbSize    interface{}      `json:"largeThumbSize"`
	
	GsColumns         []GsColumn       `json:"gsColumns"`
}

// FileData represents a single file result. Numeric JSON keys map to string indexes.
type FileData struct {
	Zero          string      `json:"0"` // Hash ID (Read!)
	One           interface{} `json:"1"`
	Two           string      `json:"2"` // File extension (Read!)
	Three         interface{} `json:"3"`
	Four          string      `json:"4"` // Size string (Read!)
	Five          string      `json:"5"` // Date string (Read!)
	Six           interface{} `json:"6"`
	Seven         interface{} `json:"7"`
	Eight         interface{} `json:"8"`
	Nine          interface{} `json:"9"`
	Ten           string      `json:"10"` // Post title (Read!)
	Eleven        string      `json:"11"` // Extension (Read!)
	Twelve        interface{} `json:"12"`
	Thirteen      interface{} `json:"13"`
	Fourteen      string      `json:"14"` // Duration string (Read!)
	Fifteen       interface{} `json:"15"`
	Sixteen       interface{} `json:"16"`
	Seventeen     interface{} `json:"17"`
	Eighteen      interface{} `json:"18"`
	Nineteen      interface{} `json:"19"`
	ThirtyFive    interface{} `json:"35"`
	Type          string      `json:"type"` // Content type (Read!)
	Height        interface{} `json:"height"`
	Width         interface{} `json:"width"`
	Theight       interface{} `json:"theight"`
	Twidth        interface{} `json:"twidth"`
	Fullres       string      `json:"fullres"` // (Read!)
	Alangs        []string    `json:"alangs"` // (Read!)
	Slangs        interface{} `json:"slangs"`
	Passwd        bool        `json:"passwd"` // (Read!)
	Virus         bool        `json:"virus"` // (Read!)
	Expires       interface{} `json:"expires"`
	Nfo           interface{} `json:"nfo"`
	Ts            int64       `json:"ts"`      // Unix timestamp (Read!)
	RawSize       int64       `json:"rawSize"` // Size in bytes (Read!)
	Volume        interface{} `json:"volume"`
	Sc            interface{} `json:"sc"`
	PrimaryURL    interface{} `json:"primaryURL"`
	FallbackURL   interface{} `json:"fallbackURL"`
	Sb            interface{} `json:"sb"`
	Size          interface{} `json:"size"`
	Runtime       interface{} `json:"runtime"`
	Sig           interface{} `json:"sig"`
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
	Num  int    `json:"num"`
	Name string `json:"name"`
}
