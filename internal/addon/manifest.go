package addon

import (
	"github.com/kiskey/stremio-easynews-go/internal/i18n"
	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

// ManifestConfigField defines a single configuration field
type ManifestConfigField struct {
	Title   string            `json:"title"`
	Key     string            `json:"key"`
	Type    string            `json:"type"` // text, password, checkbox, select, number
	Options map[string]string `json:"options,omitempty"`
	Default string            `json:"default,omitempty"`
	Hint    string            `json:"hint,omitempty"`
}

// Manifest defines the top-level Stremio addon manifest.
type Manifest struct {
	ID                  string                `json:"id"`
	Version             string                `json:"version"`
	Description         string                `json:"description"`
	Name                string                `json:"name"`
	Background          string                `json:"background"`
	Logo                string                `json:"logo"`
	BehaviorHints       ManifestBehaviorHints `json:"behaviorHints"`
	Resources           []ManifestResource    `json:"resources"`
	Types               []string              `json:"types"`
	Catalogs            []interface{}         `json:"catalogs"`
	Config              []ManifestConfigField `json:"config,omitempty"`
	StremioAddonsConfig interface{}           `json:"stremioAddonsConfig,omitempty"`
}

type ManifestBehaviorHints struct {
	Configurable          bool `json:"configurable"`
	ConfigurationRequired bool `json:"configurationRequired"`
}

type ManifestResource struct {
	Name       string   `json:"name"`
	Types      []string `json:"types,omitempty"`
	IDPrefixes []string `json:"idPrefixes,omitempty"`
}

// Read-only package-level static UI language mapping to avoid dynamic map allocations
var staticUiLanguageOptions = map[string]string{
	"eng": "English",
	"ger": "Deutsch (German)",
	"spa": "Español (Spanish)",
	"fre": "Français (French)",
	"ita": "Italiano (Italian)",
	"jpn": "日本語 (Japanese)",
	"por": "Português (Portuguese)",
	"rus": "Русский (Russian)",
	"kor": "한국어 (Korean)",
	"chi": "中文 (Chinese)",
	"dut": "Nederlands (Dutch)",
	"rum": "Română (Romanian)",
	"bul": "Български (Bulgarian)",
}

// BuildManifest constructs the Stremio manifest with default English labels.
func BuildManifest() Manifest {
	t := i18n.GetTranslations("eng")

	return Manifest{
		ID:          "community.easynews-plus-plus",
		Version:     shared.GetVersion(),
		Description: "Open-source Easynews addon for Stremio and compatible clients (Omni, Vidi, Fusion, Nuvio)",
		Name:        "Easynews++",
		Background:  "https://i.imgur.com/QPPXf5T.jpeg",
		Logo:        "https://pbs.twimg.com/profile_images/479627852757733376/8v9zH7Yo_400x400.jpeg",
		BehaviorHints: ManifestBehaviorHints{
			Configurable:          true,
			ConfigurationRequired: true,
		},
		Resources: []ManifestResource{
			{
				Name:       "stream",
				Types:      []string{"movie", "series"},
				IDPrefixes: []string{"tt"},
			},
		},
		Types:    []string{"movie", "series"},
		Catalogs: []interface{}{},
		Config: []ManifestConfigField{
			{
				Title:   t.Form.UILanguage,
				Key:     "uiLanguage",
				Type:    "select",
				Options: staticUiLanguageOptions,
				Default: "eng",
			},
			{
				Title: "Easynews " + t.Form.Username,
				Key:   "username",
				Type:  "text",
			},
			{
				Title: "Easynews " + t.Form.Password,
				Key:   "password",
				Type:  "password",
			},
			{
				Title:   t.Form.StrictTitleMatching,
				Key:     "strictTitleMatching",
				Type:    "checkbox",
				Default: "true",
				Hint:    t.Form.StrictTitleMatchingHint,
			},
			{
				Title:   t.Form.PreferredLanguage,
				Key:     "preferredLanguage",
				Type:    "select",
				Options: i18n.LanguageDisplayNames,
				Default: "",
				Hint:    t.Form.PreferredLanguageHint,
			},
			{
				Title:   t.Form.SortingMethod,
				Key:     "sortingPreference",
				Type:    "select",
				Options: sortingOptionsMap(t),
				Default: "quality_first",
				Hint:    t.Form.SortingMethodHint,
			},
			{
				Title:   t.Form.ShowQualities,
				Key:     "showQualities",
				Type:    "select",
				Options: qualityOptionsMap(t),
				Default: "4k,1080p,720p,480p",
			},
			{
				Title:   t.Form.MaxResultsPerQuality,
				Key:     "maxResultsPerQuality",
				Type:    "number",
				Default: "0",
			},
			{
				Title:   t.Form.MaxFileSize,
				Key:     "maxFileSize",
				Type:    "number",
				Default: "0",
			},
		},
	}
}

func sortingOptionsMap(t i18n.TranslationKeys) map[string]string {
	// Pre-allocated map size to prevent resizing dynamic allocations
	m := make(map[string]string, 4)
	m["quality_first"] = t.SortingOptions.QualityFirst
	m["language_first"] = t.SortingOptions.LanguageFirst
	m["size_first"] = t.SortingOptions.SizeFirst
	m["date_first"] = t.SortingOptions.DateFirst
	return m
}

func qualityOptionsMap(t i18n.TranslationKeys) map[string]string {
	// Pre-allocated map size to prevent resizing dynamic allocations
	m := make(map[string]string, 10)
	m["4k,1080p,720p,480p"] = t.QualityOptions.AllQualities
	m["4k"] = "4K/UHD/2160p"
	m["1080p"] = "1080p/FHD"
	m["720p"] = "720p/HD"
	m["480p"] = "480p/SD"
	m["4k,1080p"] = "4K + 1080p"
	m["1080p,720p"] = "1080p + 720p"
	m["720p,480p"] = "720p + 480p"
	m["4k,1080p,720p"] = "4K + 1080p + 720p"
	m["1080p,720p,480p"] = "1080p + 720p + 480p"
	return m
}
