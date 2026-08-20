package addon

// Stream represents a single stream resource returned to the Stremio protocol.
type Stream struct {
	Name          string         `json:"name"`
	URL           string         `json:"url"`
	Description   string         `json:"description,omitempty"`
	BehaviorHints *BehaviorHints `json:"behaviorHints,omitempty"`

	// SortMeta contains precomputed, structured sort fields.
	// Bypassed during JSON marshaling via the json:"-" tag.
	SortMeta *SortMeta `json:"-"`
}

// BehaviorHints provides structural hints to Stremio regarding streaming properties.
type BehaviorHints struct {
	NotWebReady bool              `json:"notWebReady,omitempty"`
	BingeGroup  string            `json:"bingeGroup,omitempty"`
	VideoSize   int64             `json:"videoSize,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Filename    string            `json:"filename,omitempty"`
}

// SortMeta structures performance properties for low-latency sorting.
type SortMeta struct {
	QualityScore        int
	SourceScore         int // 8=Remux, 7=BluRay, 6=WEB-DL, 5=WEBRip/HDTV, 4=HDRip, 3=DVDRip, 0=unknown
	HDRScore            int // 4=DV, 3=HDR10+, 2=HDR10, 1=HDR, 0=SDR
	CodecScore          int // 3=AV1, 2=HEVC, 1=AVC, 0=other
	SizeUnit            string
	SizeValue           float64
	DateMs              int64
	HasPreferredLang    bool
	IsProper            bool
	IsRepack            bool
	Edition             string
	CandidateConfidence float64
	MatchedTitle        string
	MatchedTitleSource  string
}

// TitleVariant records where a candidate title came from and how strongly it
// should be trusted. Fields are internal metadata only; they do not change the
// Stremio stream response schema.
type TitleVariant struct {
	Title      string
	Source     string
	Kind       string
	Language   string
	Country    string
	Confidence float64
}

// MetaProviderResponse is the compatibility projection consumed by the current
// query planner. AlternativeNames remains intact, while TitleVariants and
// metadata provenance provide richer information for later scoring batches.
type MetaProviderResponse struct {
	Name                string
	OriginalName        string
	AlternativeNames    []string
	TitleVariants       []TitleVariant
	MetadataSource      string
	MetadataConfidence  float64
	Year                int
	Season              string
	Episode             string
	OriginalLanguage    string
	EpisodeAirDate      string
	IsAnimation         bool
	OriginCountries     []string
	SeasonEpisodeCount  int
	SeasonEpisodeCounts map[int]int
}
