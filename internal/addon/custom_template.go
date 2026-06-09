package addon

import (
	"bytes"
	"embed"
	"html/template"
	"strings"
	"time"

	"github.com/kiskey/stremio-easynews-go/internal/i18n"
)

//go:embed template.html
var templateFS embed.FS

type templateData struct {
	Manifest         Manifest
	Translations     i18n.TranslationKeys
	TranslatedFields []ManifestConfigField
	UILanguage       string
	ISOCode          string
	CacheBreaker     int64
}

// RenderConfigurePage compiles and executes the embedded landing and configuration page HTML template.
func RenderConfigurePage(manifest Manifest) string {
	uiLang := "eng"
	for _, f := range manifest.Config {
		if f.Key == "uiLanguage" {
			uiLang = f.Default
			break
		}
	}

	translations := i18n.GetTranslations(uiLang)
	translatedFields := translateFields(manifest.Config, translations)

	data := templateData{
		Manifest:         manifest,
		Translations:     translations,
		TranslatedFields: translatedFields,
		UILanguage:       uiLang,
		ISOCode:          i18n.ConvertToISO6392(uiLang),
		CacheBreaker:     time.Now().Unix(),
	}

	tmplBytes, err := templateFS.ReadFile("template.html")
	if err != nil {
		return "<html><body>Configuration Template File Missing</body></html>"
	}

	tmpl, err := template.New("config").Funcs(template.FuncMap{
		"eq": func(a, b string) bool { return a == b },
	}).Parse(string(tmplBytes))
	if err != nil {
		return "<html><body>Configuration Template Compilation Error: " + err.Error() + "</body></html>"
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "<html><body>Configuration Execution Error: " + err.Error() + "</body></html>"
	}
	return buf.String()
}

func translateFields(fields []ManifestConfigField, t i18n.TranslationKeys) []ManifestConfigField {
	result := make([]ManifestConfigField, len(fields))
	copy(result, fields)
	for i, f := range result {
		switch f.Key {
		case "username":
			result[i].Title = "Easynews " + t.Form.Username
		case "password":
			result[i].Title = "Easynews " + t.Form.Password
		case "strictTitleMatching":
			result[i].Title = t.Form.StrictTitleMatching
			result[i].Hint = t.Form.StrictTitleMatchingHint
		case "preferredLanguage":
			result[i].Title = t.Form.PreferredLanguage
			result[i].Hint = t.Form.PreferredLanguageHint
			if result[i].Options != nil {
				newOpts := make(map[string]string)
				for k, v := range result[i].Options {
					if k == "" && t.Languages.NoPreference != "" {
						newOpts[k] = t.Languages.NoPreference
					} else {
						newOpts[k] = v
					}
				}
				result[i].Options = newOpts
			}
		case "sortingPreference":
			result[i].Title = t.Form.SortingMethod
			result[i].Hint = t.Form.SortingMethodHint
			if result[i].Options != nil {
				newOpts := make(map[string]string)
				for k, v := range result[i].Options {
					switch k {
					case "quality_first":
						newOpts[k] = t.SortingOptions.QualityFirst
					case "language_first":
						newOpts[k] = t.SortingOptions.LanguageFirst
					case "size_first":
						newOpts[k] = t.SortingOptions.SizeFirst
					case "date_first":
						newOpts[k] = t.SortingOptions.DateFirst
					default:
						newOpts[k] = v
					}
				}
				result[i].Options = newOpts
			}
		case "uiLanguage":
			result[i].Title = t.Form.UILanguage
		case "showQualities":
			result[i].Title = t.Form.ShowQualities
			if result[i].Options != nil {
				newOpts := make(map[string]string)
				for k, v := range result[i].Options {
					if k == "4k,1080p,720p,480p" && t.QualityOptions.AllQualities != "" {
						newOpts[k] = t.QualityOptions.AllQualities
					} else {
						newOpts[k] = v
					}
				}
				result[i].Options = newOpts
			}
		case "maxResultsPerQuality":
			result[i].Title = t.Form.MaxResultsPerQuality
		case "maxFileSize":
			result[i].Title = t.Form.MaxFileSize
		}
	}
	return result
}
