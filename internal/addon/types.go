package addon

// Stream represents a single stream resource returned to the Stremio protocol.
type Stream struct {
    Name          string         `json:"name"`
    URL           string         `json:"url"`
    Description   string         `json:"description,omitempty"`
    BehaviorHints *BehaviorHints `json:"behaviorHints,omitempty"`
    
    // SortMeta contains precomputed, structured sort fields.
    // Bypassed during JSON marshaling via the json:"-" tag.
    SortMeta      *SortMeta      `json:"-"`
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
    QualityScore     int
    SizeUnit         string  // "GB", "MB", or ""
    SizeValue        float64
    DateMs           int64
    HasPreferredLang bool
    IsProper         bool
    IsRepack         bool
    Edition          string
}

// MetaProviderResponse represents the standard metadata structure returned by providers (IMDb/Cinemeta).
type MetaProviderResponse struct {
    Name             string
    OriginalName     string
    AlternativeNames []string
    Year             int
    Season           string
    Episode          string
    OriginalLanguage string
    EpisodeAirDate   string
}
