package addon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/kiskey/stremio-easynews-go/internal/i18n"
	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

var metaLogger = shared.CreateLogger("Meta", "")

var (
	tmdbAPIKey = os.Getenv("TMDB_API_KEY")
	useTMDB    = tmdbAPIKey != ""
)

const metaFetchTimeout = 5000 * time.Millisecond

// ---------------------------------------------------------------------------
// TMDB Title Translation Logic
// ---------------------------------------------------------------------------

func getTMDBTranslatedTitle(imdbID, preferredLanguage string) (string, error) {
	if !useTMDB || preferredLanguage == "" {
		return "", nil
	}

	tmdbLang := i18n.ConvertToTMDBLanguageCode(preferredLanguage)

	ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
	defer cancel()

	// Step 1: Find TMDB ID from IMDb ID
	findURL := fmt.Sprintf("https://api.themoviedb.org/3/find/%s?api_key=%s&external_source=imdb_id", imdbID, tmdbAPIKey)
	req, _ := http.NewRequestWithContext(ctx, "GET", findURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		useTMDB = false
		return "", fmt.Errorf("TMDB API key invalid")
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("TMDB find error: %d", resp.StatusCode)
	}

	var findData struct {
		MovieResults []struct {
			ID int `json:"id"`
		} `json:"movie_results"`
		TVResults []struct {
			ID int `json:"id"`
		} `json:"tv_results"`
	}
	if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&findData); err != nil {
		return "", err
	}

	isMovie := len(findData.MovieResults) > 0
	isTV := len(findData.TVResults) > 0
	if !isMovie && !isTV {
		return "", nil
	}

	var tmdbID int
	if isMovie {
		tmdbID = findData.MovieResults[0].ID
	} else {
		tmdbID = findData.TVResults[0].ID
	}

	// Step 2: Get details in preferred language
	var detailsURL string
	if isMovie {
		detailsURL = fmt.Sprintf("https://api.themoviedb.org/3/movie/%d?api_key=%s&language=%s", tmdbID, tmdbAPIKey, tmdbLang)
	} else {
		detailsURL = fmt.Sprintf("https://api.themoviedb.org/3/tv/%d?api_key=%s&language=%s", tmdbID, tmdbAPIKey, tmdbLang)
	}

	req2, _ := http.NewRequestWithContext(ctx, "GET", detailsURL, nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return "", err
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != 200 {
		return "", fmt.Errorf("TMDB details error: %d", resp2.StatusCode)
	}

	var details struct {
		Title string `json:"title"`
		Name  string `json:"name"`
	}
	if err := sonic.ConfigStd.NewDecoder(resp2.Body).Decode(&details); err != nil {
		return "", err
	}

	if details.Title != "" {
		return details.Title, nil
	}
	if details.Name != "" {
		return details.Name, nil
	}

	// Step 3: Try translations endpoint as fallback
	var transURL string
	if isMovie {
		transURL = fmt.Sprintf("https://api.themoviedb.org/3/movie/%d/translations?api_key=%s", tmdbID, tmdbAPIKey)
	} else {
		transURL = fmt.Sprintf("https://api.themoviedb.org/3/tv/%d/translations?api_key=%s", tmdbID, tmdbAPIKey)
	}

	req3, _ := http.NewRequestWithContext(ctx, "GET", transURL, nil)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		return "", err
	}
	defer resp3.Body.Close()

	var transData struct {
		Translations []struct {
			ISO639_1 string `json:"iso_639_1"`
			Data     struct {
				Title string `json:"title"`
				Name  string `json:"name"`
			} `json:"data"`
		} `json:"translations"`
	}
	if err := sonic.ConfigStd.NewDecoder(resp3.Body).Decode(&transData); err != nil {
		return "", err
	}

	for _, t := range transData.Translations {
		if t.ISO639_1 == tmdbLang {
			if isMovie && t.Data.Title != "" {
				return t.Data.Title, nil
			}
			if !isMovie && t.Data.Name != "" {
				return t.Data.Name, nil
			}
		}
	}

	return "", nil
}

// ---------------------------------------------------------------------------
// IMDb Metadata Lookup Provider
// ---------------------------------------------------------------------------

func imdbMetaProvider(id, preferredLanguage string) (MetaProviderResponse, error) {
	parts := strings.Split(id, ":")
	tt := parts[0]
	var season, episode string
	if len(parts) > 1 {
		season = parts[1]
	}
	if len(parts) > 2 {
		episode = parts[2]
	}

	ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
	defer cancel()

	url := fmt.Sprintf("https://v2.sg.media-imdb.com/suggestion/t/%s.json", tt)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return MetaProviderResponse{}, err
	}
	defer resp.Body.Close()

	var data struct {
		D []struct {
			ID string `json:"id"`
			L  string `json:"l"`
			Y  int    `json:"y"`
		} `json:"d"`
	}
	if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&data); err != nil {
		return MetaProviderResponse{}, err
	}

	var item struct {
		L string
		Y int
	}
	found := false
	for _, d := range data.D {
		if d.ID == tt {
			item.L = d.L
			item.Y = d.Y
			found = true
			break
		}
	}
	if !found {
		return MetaProviderResponse{}, fmt.Errorf("no IMDb match for %s", tt)
	}

	originalName := item.L
	alternatives := GetAlternativeTitles(originalName, nil)

	if preferredLanguage != "" {
		translated, err := getTMDBTranslatedTitle(tt, preferredLanguage)
		if err == nil && translated != "" {
			hasIt := false
			for _, a := range alternatives {
				if a == translated {
					hasIt = true
					break
				}
			}
			if !hasIt {
				alternatives = append(alternatives, translated)
			}
			sanitized := SanitizeTitle(translated)
			if sanitized != translated {
				hasSanitized := false
				for _, a := range alternatives {
					if a == sanitized {
						hasSanitized = true
						break
					}
				}
				if !hasSanitized {
					alternatives = append(alternatives, sanitized)
				}
			}
		}
	}

	return MetaProviderResponse{
		Name:             originalName,
		OriginalName:     originalName,
		AlternativeNames: alternatives,
		Year:             item.Y,
		Season:           season,
		Episode:          episode,
	}, nil
}

// ---------------------------------------------------------------------------
// Cinemeta Metadata Lookup Provider (Fallback)
// ---------------------------------------------------------------------------

func cinemetaMetaProvider(id, contentType, preferredLanguage string) (MetaProviderResponse, error) {
	parts := strings.Split(id, ":")
	tt := parts[0]
	var season, episode string
	if len(parts) > 1 {
		season = parts[1]
	}
	if len(parts) > 2 {
		episode = parts[2]
	}

	ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
	defer cancel()

	url := fmt.Sprintf("https://v3-cinemeta.strem.io/meta/%s/%s.json", contentType, tt)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return MetaProviderResponse{}, err
	}
	defer resp.Body.Close()

	var data struct {
		Meta struct {
			Name        string `json:"name"`
			Year        string `json:"year"`
			ReleaseInfo string `json:"releaseInfo"`
		} `json:"meta"`
	}
	if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&data); err != nil {
		return MetaProviderResponse{}, err
	}

	name := data.Meta.Name
	year := ExtractDigits(data.Meta.Year)
	if year == nil {
		year = ExtractDigits(data.Meta.ReleaseInfo)
	}
	yearVal := 0
	if year != nil {
		yearVal = *year
	}

	alternatives := GetAlternativeTitles(name, nil)

	if preferredLanguage != "" {
		translated, err := getTMDBTranslatedTitle(tt, preferredLanguage)
		if err == nil && translated != "" {
			hasIt := false
			for _, a := range alternatives {
				if a == translated {
					hasIt = true
					break
				}
			}
			if !hasIt {
				alternatives = append(alternatives, translated)
			}
		}
	}

	return MetaProviderResponse{
		Name:             name,
		OriginalName:     name,
		AlternativeNames: alternatives,
		Year:             yearVal,
		Season:           season,
		Episode:          episode,
	}, nil
}

// ---------------------------------------------------------------------------
// Public Metadata Gateway Interface
// ---------------------------------------------------------------------------

func PublicMetaProvider(id, contentType, preferredLanguage string) (MetaProviderResponse, error) {
	meta, err := imdbMetaProvider(id, preferredLanguage)
	if err == nil && meta.Name != "" {
		return meta, nil
	}

	metaLogger.Debug("IMDb metadata lookup failed, falling back to Cinemeta: %v", err)

	meta, err = cinemetaMetaProvider(id, contentType, preferredLanguage)
	if err == nil && meta.Name != "" {
		return meta, nil
	}

	return MetaProviderResponse{}, fmt.Errorf("failed to find metadata for %s", id)
}
