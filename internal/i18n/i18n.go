package i18n

import "strings"

// Language is one of our supported UI languages.
type Language string

const (
    EN Language = "en"
    DE Language = "de"
    ES Language = "es"
    FR Language = "fr"
    IT Language = "it"
    JA Language = "ja"
    PT Language = "pt"
    RU Language = "ru"
    KO Language = "ko"
    ZH Language = "zh"
    NL Language = "nl"
    RO Language = "ro"
    BG Language = "bg"
)

// TranslationKeys holds all translatable strings.
type TranslationKeys struct {
    ConfigPage     ConfigPage
    Form           Form
    Languages      Languages
    SortingOptions SortingOptions
    QualityOptions QualityOptions
    Errors         Errors
}

type ConfigPage struct {
    Title        string
    CopyConfig   string
    AddToStremio string
    ConfigCopied string
    Version      string
    Description  string
}

type Form struct {
    Username                string
    Password                string
    StrictTitleMatching     string
    StrictTitleMatchingHint string
    PreferredLanguage       string
    PreferredLanguageHint   string
    SortingMethod           string
    SortingMethodHint       string
    UILanguage              string
    ShowQualities           string
    MaxResultsPerQuality    string
    MaxFileSize             string
    NoLimit                 string
}

type Languages struct {
    NoPreference string
    English      string
    German       string
    Spanish      string
    French       string
    Italian      string
    Japanese     string
    Portuguese   string
    Russian      string
    Korean       string
    Chinese      string
    Dutch        string
    Romanian     string
    Bulgarian    string
}

type SortingOptions struct {
    QualityFirst  string
    LanguageFirst string
    SizeFirst     string
    DateFirst     string
}

type QualityOptions struct {
    AllQualities string
}

type Errors struct {
    AuthFailed string
}

// ---------------------------------------------------------------------------
// Thread-Safe Read-Only Package Global Maps (Zero-allocation at runtime)
// ---------------------------------------------------------------------------

var ISOToLanguage = map[string]Language{
    "eng": EN, "ger": DE, "spa": ES, "fre": FR, "ita": IT,
    "jpn": JA, "por": PT, "rus": RU, "kor": KO, "chi": ZH,
    "dut": NL, "rum": RO, "bul": BG,
    "":    EN,
}

var AdditionalLanguageCodes = map[string]string{
    "ara": "ar", "cze": "cs", "dan": "da", "fin": "fi", "gre": "el",
    "heb": "he", "hin": "hi", "hun": "hu", "ice": "is", "ind": "id",
    "may": "ms", "nor": "no", "per": "fa", "pol": "pl", "swe": "sv",
    "tha": "th", "tur": "tr", "ukr": "uk", "vie": "vi",
}

var iso1Map = map[Language]string{
    EN: "en", DE: "de", ES: "es", FR: "fr", IT: "it",
    JA: "ja", PT: "pt", RU: "ru", KO: "ko", ZH: "zh",
    NL: "nl", RO: "ro", BG: "bg",
}

var iso1ToISO3Map = map[string]string{
    "en": "eng", "de": "ger", "es": "spa", "fr": "fre", "it": "ita",
    "ja": "jpn", "pt": "por", "ru": "rus", "ko": "kor", "zh": "chi",
    "nl": "dut", "ro": "rum", "bg": "bul",
}

// DEFAULT_UI_LANGUAGE represents the default 3-letter UI language code.
const DEFAULT_UI_LANGUAGE = "eng"

// SanitizeUiLanguage validates untrusted language query parameters.
func SanitizeUiLanguage(input string) string {
    if input == "" {
        return DEFAULT_UI_LANGUAGE
    }
    if _, ok := ISOToLanguage[input]; ok {
        return input
    }
    return DEFAULT_UI_LANGUAGE
}

// GetUILanguage converts a preferredLanguage string configuration value to internal Language.
func GetUILanguage(preferredLanguage string) Language {
    if preferredLanguage == "" {
        return EN
    }
    if lang, ok := ISOToLanguage[preferredLanguage]; ok {
        return lang
    }
    return EN
}

// GetTranslations returns translations for a given 3-letter ISO 639-2 language code.
func GetTranslations(langCode string) TranslationKeys {
    lang, ok := ISOToLanguage[langCode]
    if !ok {
        lang = EN
    }
    if t, ok := translations[lang]; ok {
        return t
    }
    return translations[EN]
}

// LanguageDisplayNames configures names displayed in selection dropdowns.
var LanguageDisplayNames = map[string]string{
    "":    "No preference",
    "eng": "English", "ger": "Deutsch (German)", "spa": "Español (Spanish)",
    "fre": "Français (French)", "ita": "Italiano (Italian)", "jpn": "日本語 (Japanese)",
    "por": "Português (Portuguese)", "rus": "Русский (Russian)", "kor": "한국어 (Korean)",
    "chi": "中文 (Chinese)", "dut": "Nederlands (Dutch)", "rum": "Română (Romanian)",
    "bul": "Български (Bulgarian)",
    "ara": "Arabic (العربية)", "cze": "Czech (Čeština)", "dan": "Danish (Dansk)",
    "fin": "Finnish (Suomi)", "gre": "Greek (Ελληνικά)", "heb": "Hebrew (עברית)",
    "hin": "Hindi (हिन्दी)", "hun": "Hungarian (Magyar)", "ice": "Icelandic (Íslenska)",
    "ind": "Indonesian (Bahasa Indonesia)", "may": "Malay (Bahasa Melayu)",
    "nor": "Norwegian (Norsk)", "per": "Persian (فارسی)", "pol": "Polish (Polski)",
    "swe": "Swedish (Svenska)", "tha": "Thai (ไทย)", "tur": "Turkish (Türkçe)",
    "ukr": "Ukrainian (Українська)", "vie": "Vietnamese (Tiếng Việt)",
}

// --- ALL TRANSLATIONS ---

var translations = map[Language]TranslationKeys{
    EN: {
        ConfigPage: ConfigPage{
            Title:        "Configuration",
            CopyConfig:   "Copy Configuration",
            AddToStremio: "Add to Stremio",
            ConfigCopied: "Copied!",
            Version:      "Version",
            Description:  "Open-source Easynews addon for Stremio and compatible apps (Omni, Vidi, Fusion, Nuvio). It searches Easynews and returns playable streams, with smart title matching, quality sorting, language filtering and self-hosting. Contribute on <a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a>.",
        },
        Form: Form{
            Username:                "Username",
            Password:                "Password",
            StrictTitleMatching:     "Strict Title Matching",
            StrictTitleMatchingHint: "Recommended: Filters out results that don't exactly match the movie or series title",
            PreferredLanguage:       "Preferred Audio Language",
            PreferredLanguageHint:   "Used to find and prioritize content with localized titles in the preferred language",
            SortingMethod:           "Sorting Method",
            SortingMethodHint:       "All options use the same relevance-first API search, then sort results locally",
            UILanguage:              "UI Language",
            ShowQualities:           "Qualities to show in streams list",
            MaxResultsPerQuality:    "Max results per quality",
            MaxFileSize:             "Max file size in GB",
            NoLimit:                 "No limit",
        },
        Languages: Languages{
            NoPreference: "No preference",
            English:      "English",
            German:       "German (Deutsch)",
            Spanish:      "Spanish (Español)",
            French:       "French (Français)",
            Italian:      "Italian (Italiano)",
            Japanese:     "Japanese (日本語)",
            Portuguese:   "Portuguese (Português)",
            Russian:      "Russian (Русский)",
            Korean:       "Korean (한국어)",
            Chinese:      "Chinese (中文)",
            Dutch:        "Dutch (Nederlands)",
            Romanian:     "Romanian (Română)",
            Bulgarian:    "Bulgarian (Български)",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "Quality (4K → 1080p → 720p)",
            LanguageFirst: "Preferred Language, then Quality",
            SizeFirst:     "File Size (largest first)",
            DateFirst:     "Date Added (newest first)",
        },
        QualityOptions: QualityOptions{AllQualities: "All Qualities"},
        Errors:         Errors{AuthFailed: "Authentication Failed: Invalid username or password\nCheck your credentials & reconfigure addon"},
    },
    DE: {
        ConfigPage: ConfigPage{
            Title:        "Konfiguration",
            CopyConfig:   "Konfiguration kopieren",
            AddToStremio: "Zu Stremio hinzufügen",
            ConfigCopied: "Kopiert!",
            Version:      "Version",
            Description:  "Open-Source-Easynews-Addon für Stremio und kompatible Apps (Omni, Vidi, Fusion, Nuvio). Es durchsucht Easynews und gibt abspielbare Streams mit intelligenter Titelübereinstimmung, Qualitätssortierung, Sprachfilterung und Self-Hosting zurück. Tragen Sie auf <a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a> bei.",
        },
        Form: Form{
            Username:                "Benutzername",
            Password:                "Passwort",
            StrictTitleMatching:     "Strikte Titelsuche",
            StrictTitleMatchingHint: "Empfohlen: Filtert Ergebnisse heraus, die nicht genau dem Film- oder Serientitel entsprechen",
            PreferredLanguage:       "Bevorzugte Audiosprache",
            PreferredLanguageHint:   "Wird verwendet, um Inhalte mit lokalisierten Titeln in der bevorzugten Sprache zu finden und zu priorisieren",
            SortingMethod:           "Sortiermethode",
            SortingMethodHint:       "Alle Optionen nutzen dieselbe relevante API-Suche und sortieren die Ergebnisse lokal",
            UILanguage:              "UI-Sprache",
            ShowQualities:           "Qualitäten in der Streamliste anzeigen",
            MaxResultsPerQuality:    "Maximale Ergebnisse pro Qualität",
            MaxFileSize:             "Maximale Dateigröße in GB",
            NoLimit:                 "Keine Begrenzung",
        },
        Languages: Languages{
            NoPreference: "Keine Präferenz",
            English:      "Englisch",
            German:       "Deutsch",
            Spanish:      "Spanisch (Español)",
            French:       "Französisch (Français)",
            Italian:      "Italienisch (Italiano)",
            Japanese:     "Japanisch (日本語)",
            Portuguese:   "Portugiesisch (Português)",
            Russian:      "Russisch (Русский)",
            Korean:       "Koreanisch (한국어)",
            Chinese:      "Chinesisch (中文)",
            Dutch:        "Niederländisch (Nederlands)",
            Romanian:     "Rumänisch (Română)",
            Bulgarian:    "Bulgarisch (Български)",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "Qualität (4K → 1080p → 720p)",
            LanguageFirst: "Bevorzugte Sprache, dann Qualität",
            SizeFirst:     "Dateigröße (größte zuerst)",
            DateFirst:     "Hinzugefügt am (neueste zuerst)",
        },
        QualityOptions: QualityOptions{AllQualities: "Alle Qualitäten"},
        Errors:         Errors{AuthFailed: "Authentifizierung fehlgeschlagen: Ungültiger Benutzername oder Passwort\nÜberprüfen Sie Ihre Zugangsdaten und konfigurieren Sie das Addon neu"},
    },
    ES: {
        ConfigPage: ConfigPage{
            Title:        "Configuración",
            CopyConfig:   "Copiar configuración",
            AddToStremio: "Añadir a Stremio",
            ConfigCopied: "¡Copiado!",
            Version:      "Versión",
            Description:  "Addon de código abierto de Easynews para Stremio y aplicaciones compatibles (Omni, Vidi, Fusion, Nuvio). Busca en Easynews y devuelve transmisiones reproducibles, con coincidencia inteligente de títulos, ordenación por calidad, filtrado de idioma y auto-alojamiento. Contribuye en <a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a>.",
        },
        Form: Form{
            Username:                "Usuario",
            Password:                "Contraseña",
            StrictTitleMatching:     "Coincidencia estricta de título",
            StrictTitleMatchingHint: "Recomendado: Filtra los resultados que no coinciden exactamente con el título de la película o serie",
            PreferredLanguage:       "Idioma de audio preferido",
            PreferredLanguageHint:   "Se usa para buscar y priorizar contenido con títulos localizados en el idioma preferido",
            SortingMethod:           "Método de ordenación",
            SortingMethodHint:       "Todas las opciones usan la misma búsqueda de API primero relevante, luego ordenan localmente",
            UILanguage:              "Idioma de la interfaz",
            ShowQualities:           "Qualities to show in streams list",
            MaxResultsPerQuality:    "Max results per quality",
            MaxFileSize:             "Tamaño máximo de archivo en GB",
            NoLimit:                 "Sin límite",
        },
        Languages: Languages{
            NoPreference: "Sin preferencia",
            English:      "Inglés",
            German:       "Alemán (Deutsch)",
            Spanish:      "Español",
            French:       "Francés (Français)",
            Italian:      "Italiano",
            Japanese:     "Japonés (日本語)",
            Portuguese:   "Portugués (Português)",
            Russian:      "Ruso (Русский)",
            Korean:       "Coreano (한국어)",
            Chinese:      "Chino (中文)",
            Dutch:        "Neerlandés (Nederlands)",
            Romanian:     "Rumano (Română)",
            Bulgarian:    "Búlgaro (Български)",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "Calidad (4K → 1080p → 720p)",
            LanguageFirst: "Idioma preferido, luego Calidad",
            SizeFirst:     "Tamaño de archivo (más grande primero)",
            DateFirst:     "Fecha de añadido (más reciente primero)",
        },
        QualityOptions: QualityOptions{AllQualities: "Todas las calidades"},
        Errors:         Errors{AuthFailed: "Error de autenticación: usuario o contraseña incorrectos\nVerifique sus credenciales y vuelva a configurar el addon"},
    },
    FR: {
        ConfigPage: ConfigPage{
            Title:        "Configuration",
            CopyConfig:   "Copier la configuration",
            AddToStremio: "Ajouter à Stremio",
            ConfigCopied: "Copié !",
            Version:      "Version",
            Description:  "Extension Easynews open-source pour Stremio et les applications compatibles (Omni, Vidi, Fusion, Nuvio). Elle recherche sur Easynews et renvoie des flux lisibles, avec mise en correspondance intelligente des titres, tri par qualité, filtrage linguistique et auto-hébergement. Contribuez sur <a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a>.",
        },
        Form: Form{
            Username:                "Nom d'utilisateur",
            Password:                "Mot de passe",
            StrictTitleMatching:     "Correspondance stricte des titres",
            StrictTitleMatchingHint: "Recommandé : Filtre les résultats qui ne correspondent pas exactement au titre du film ou de la série",
            PreferredLanguage:       "Langue audio préférée",
            PreferredLanguageHint:   "Utilisé pour trouver et prioriser les contenus avec des titres localisés dans la langue préférée",
            SortingMethod:           "Méthode de tri",
            SortingMethodHint:       "Toutes les options utilisent la même recherche API axée sur la pertinence, puis trient localement",
            UILanguage:              "Langue de l'interface",
            ShowQualities:           "Qualités à afficher dans la liste",
            MaxResultsPerQuality:    "Résultats max par qualité",
            MaxFileSize:             "Taille maximale du fichier en GB",
            NoLimit:                 "Sans limite",
        },
        Languages: Languages{
            NoPreference: "Pas de préférence",
            English:      "Anglais",
            German:       "Allemand (Deutsch)",
            Spanish:      "Espagnol (Español)",
            French:       "Français",
            Italian:      "Italien (Italiano)",
            Japanese:     "Japonais (日本語)",
            Portuguese:   "Portugais (Português)",
            Russian:      "Russe (Русский)",
            Korean:       "Coréen (한국어)",
            Chinese:      "Chinois (中文)",
            Dutch:        "Néerlandais (Nederlands)",
            Romanian:     "Roumain (Română)",
            Bulgarian:    "Bulgare (Български)",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "Qualité (4K → 1080p → 720p)",
            LanguageFirst: "Langue préférée, puis Qualité",
            SizeFirst:     "Taille du fichier (le plus grand d'abord)",
            DateFirst:     "Date d'ajout (le plus récent d'abord)",
        },
        QualityOptions: QualityOptions{AllQualities: "Toutes les qualités"},
        Errors:         Errors{AuthFailed: "Échec de l'authentification : Nom d'utilisateur ou mot de passe invalide\nVérifiez vos identifiants et reconfigurez l'extension"},
    },
    IT: {
        ConfigPage: ConfigPage{
            Title:        "Configurazione",
            CopyConfig:   "Copia configurazione",
            AddToStremio: "Aggiungi a Stremio",
            ConfigCopied: "Copiato!",
            Version:      "Versione",
            Description:  "Addon Easynews open-source per Stremio e app compatibili (Omni, Vidi, Fusion, Nuvio). Cerca su Easynews e restituisce stream riproducibili, con corrispondenza intelligente dei titoli, ordinamento per qualità, filtraggio della lingua e auto-hosting. Contribuisci su <a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a>.",
        },
        Form: Form{
            Username:                "Nome utente",
            Password:                "Password",
            StrictTitleMatching:     "Corrispondenza esatta del titolo",
            StrictTitleMatchingHint: "Consigliato: Filtra i risultati che non corrispondono esattamente al titolo del film o della serie",
            PreferredLanguage:       "Lingua audio preferita",
            PreferredLanguageHint:   "Utilizzato per trovare e dare priorità ai contenuti con titoli localizzati nella lingua preferita",
            SortingMethod:           "Metodo di ordinamento",
            SortingMethodHint:       "Tutte le opzioni utilizzano la stessa ricerca API orientata alla rilevanza, quindi ordinano localmente",
            UILanguage:              "Lingua dell'interfaccia",
            ShowQualities:           "Qualità da mostrare nell'elenco",
            MaxResultsPerQuality:    "Risultati massimi per qualità",
            MaxFileSize:             "Dimensione massima del file in GB",
            NoLimit:                 "Senza limite",
        },
        Languages: Languages{
            NoPreference: "Nessuna preferenza",
            English:      "Inglese",
            German:       "Tedesco (Deutsch)",
            Spanish:      "Spagnolo (Español)",
            French:       "Francese (Français)",
            Italian:      "Italiano",
            Japanese:     "Giapponese (日本語)",
            Portuguese:   "Portoghese (Português)",
            Russian:      "Russo (Русский)",
            Korean:       "Coreano (한국어)",
            Chinese:      "Cinese (中文)",
            Dutch:        "Olandese (Nederlands)",
            Romanian:     "Rumeno (Română)",
            Bulgarian:    "Bulgaro (Български)",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "Qualità (4K → 1080p → 720p)",
            LanguageFirst: "Lingua preferita, poi Qualità",
            SizeFirst:     "Dimensione file (più grande prima)",
            DateFirst:     "Data di aggiunta (più recente prima)",
        },
        QualityOptions: QualityOptions{AllQualities: "Tutte le qualità"},
        Errors:         Errors{AuthFailed: "Autenticazione fallita: Nome utente o password non validi\nVerifica le tue credenziali e riconfigura l'addon"},
    },
    JA: {
        ConfigPage: ConfigPage{
            Title:        "設定",
            CopyConfig:   "設定をコピー",
            AddToStremio: "Stremioに追加",
            ConfigCopied: "コピーしました！",
            Version:      "バージョン",
            Description:  "Stremioおよび互換アプリ（Omni、Vidi、Fusion、Nuvio）用のオープンソースのEasynewsアドオン。スマートなタイトル一致、品質ソート、言語フィルタリング、セルフホスティングを備え、Easynewsを検索して再生可能なストリームを返します。<a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a>で貢献してください。",
        },
        Form: Form{
            Username:                "ユーザー名",
            Password:                "パスワード",
            StrictTitleMatching:     "厳密なタイトル一致",
            StrictTitleMatchingHint: "推奨：映画やシリーズのタイトルと正確に一致しない結果をフィルタリングして除外します",
            PreferredLanguage:       "優先オーディオ言語",
            PreferredLanguageHint:   "優先言語でローカライズされたタイトルを持つコンテンツを検索して優先するために使用されます",
            SortingMethod:           "ソート方法",
            SortingMethodHint:       "すべてのオプションは同じ関連性優先のAPI検索を使用し、その後結果をローカルでソートします",
            UILanguage:              "UI言語",
            ShowQualities:           "ストリームリストに表示する品質",
            MaxResultsPerQuality:    "品質ごとの最大結果数",
            MaxFileSize:             "最大ファイルサイズ（GB）",
            NoLimit:                 "制限なし",
        },
        Languages: Languages{
            NoPreference: "指定なし",
            English:      "英語",
            German:       "ドイツ語 (Deutsch)",
            Spanish:      "スペイン語 (Español)",
            French:       "フランス語 (Français)",
            Italian:      "イタリア語 (Italiano)",
            Japanese:     "日本語",
            Portuguese:   "ポルトガル語 (Português)",
            Russian:      "ロシア語 (Русский)",
            Korean:       "韓国語 (한국어)",
            Chinese:      "中国語 (中文)",
            Dutch:        "オランダ語 (Nederlands)",
            Romanian:     "ルーマニア語 (Română)",
            Bulgarian:    "ブルガリア語 (Български)",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "品質優先 (4K → 1080p → 720p)",
            LanguageFirst: "優先言語を優先、次に品質",
            SizeFirst:     "ファイルサイズ優先 (大きい順)",
            DateFirst:     "追加日優先 (新しい順)",
        },
        QualityOptions: QualityOptions{AllQualities: "すべての品質"},
        Errors:         Errors{AuthFailed: "認証に失敗しました：ユーザー名またはパスワードが無効です\n資格情報を確認してアドオンを再設定してください"},
    },
    PT: {
        ConfigPage: ConfigPage{
            Title:        "Configuração",
            CopyConfig:   "Copiar configuração",
            AddToStremio: "Adicionar ao Stremio",
            ConfigCopied: "Copiado!",
            Version:      "Versão",
            Description:  "Addon Easynews de código aberto para Stremio e aplicações compatíveis (Omni, Vidi, Fusion, Nuvio). Pesquisa no Easynews e devolve transmissões reproduzíveis, com correspondência inteligente de títulos, ordenação por qualidade, filtragem de idioma e auto-alojamento. Contribua no <a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a>.",
        },
        Form: Form{
            Username:                "Usuário",
            Password:                "Senha",
            StrictTitleMatching:     "Correspondência estrita de título",
            StrictTitleMatchingHint: "Recomendado: Filtra resultados que não correspondam exatamente ao título do filme ou série",
            PreferredLanguage:       "Idioma de áudio preferido",
            PreferredLanguageHint:   "Utilizado para encontrar e priorizar conteúdo com títulos localizados no idioma preferido",
            SortingMethod:           "Método de ordenação",
            SortingMethodHint:       "Todas as opções usam a mesma busca de API primeiro relevante, depois ordenam localmente",
            UILanguage:              "Idioma da interface",
            ShowQualities:           "Qualidades a mostrar na lista",
            MaxResultsPerQuality:    "Resultados máximos por qualidade",
            MaxFileSize:             "Tamanho máximo do arquivo em GB",
            NoLimit:                 "Sem limite",
        },
        Languages: Languages{
            NoPreference: "Sem preferência",
            English:      "Inglês",
            German:       "Alemão (Deutsch)",
            Spanish:      "Espanhol (Español)",
            French:       "Francês (Français)",
            Italian:      "Italiano",
            Japanese:     "Japonês (日本語)",
            Portuguese:   "Português",
            Russian:      "Russo (Русский)",
            Korean:       "Coreano (한국어)",
            Chinese:      "Chinês (中文)",
            Dutch:        "Holandês (Nederlands)",
            Romanian:     "Romeno (Română)",
            Bulgarian:    "Búlgaro (Български)",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "Qualidade (4K → 1080p → 720p)",
            LanguageFirst: "Idioma preferido, depois Qualidade",
            SizeFirst:     "Tamanho de arquivo (maior primeiro)",
            DateFirst:     "Adicionado em (mais recente primeiro)",
        },
        QualityOptions: QualityOptions{AllQualities: "Todas as qualidades"},
        Errors:         Errors{AuthFailed: "Falha na autenticação: Usuário ou senha inválidos\nVerifique suas credenciais e reconfigure o addon"},
    },
    RU: {
        ConfigPage: ConfigPage{
            Title:        "Конфигурация",
            CopyConfig:   "Копировать конфигурацию",
            AddToStremio: "Добавить в Stremio",
            ConfigCopied: "Скопировано!",
            Version:      "Версия",
            Description:  "Аддон Easynews с открытым исходным кодом для Stremio и совместимых приложений (Omni, Vidi, Fusion, Nuvio). Он ищет в Easynews и возвращает воспроизводимые потоки с умным сопоставлением названий, сортировкой по качеству, фильтрацией языков и возможностью самостоятельного хостинга. Внесите свой вклад на <a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a>.",
        },
        Form: Form{
            Username:                "Имя пользователя",
            Password:                "Пароль",
            StrictTitleMatching:     "Строгое соответствие названий",
            StrictTitleMatchingHint: "Рекомендуется: Отфильтровывает результаты, которые не соответствуют в точности названию фильма или сериала",
            PreferredLanguage:       "Предпочтительный язык аудио",
            PreferredLanguageHint:   "Используется для поиска и приоритизации контента с локализованными названиями на предпочтительном языке",
            SortingMethod:           "Метод сортировки",
            SortingMethodHint:       "Все опции используют один и тот же поиск по релевантности через API, а затем сортируют результаты локально",
            UILanguage:              "Язык интерфейса",
            ShowQualities:           "Качество для отображения в списке",
            MaxResultsPerQuality:    "Максимум результатов на одно качество",
            MaxFileSize:             "Максимальный размер файла в ГБ",
            NoLimit:                 "Без ограничений",
        },
        Languages: Languages{
            NoPreference: "Нет предпочтений",
            English:      "Английский",
            German:       "Немецкий (Deutsch)",
            Spanish:      "Испанский (Español)",
            French:       "Французский (Français)",
            Italian:      "Итальянский (Italiano)",
            Japanese:     "Японский (日本語)",
            Portuguese:   "Португальский (Português)",
            Russian:      "Русский",
            Korean:       "Корейский (한국어)",
            Chinese:      "Китайский (中文)",
            Dutch:        "Нидерландский (Nederlands)",
            Romanian:     "Румынский (Română)",
            Bulgarian:    "Болгарский (Български)",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "Качество (4K → 1080p → 720p)",
            LanguageFirst: "Предпочтительный язык, затем Качество",
            SizeFirst:     "Размер файла (сначала крупные)",
            DateFirst:     "Дата добавления (сначала новые)",
        },
        QualityOptions: QualityOptions{AllQualities: "Все качества"},
        Errors:         Errors{AuthFailed: "Ошибка авторизации: неверное имя пользователя или пароль\nПроверьте свои учетные данные и перенастройте аддон"},
    },
    KO: {
        ConfigPage: ConfigPage{
            Title:        "설정",
            CopyConfig:   "설정 복사",
            AddToStremio: "Stremio에 추가",
            ConfigCopied: "복사됨!",
            Version:      "버전",
            Description:  "Stremio 및 호환 앱(Omni, Vidi, Fusion, Nuvio)을 위한 오픈 소스 Easynews 애드온입니다. Easynews를 검색하여 재생 가능한 스트림을 반환하며, 스마트 제목 매칭, 화질 정렬, 언어 필터링 및 셀프 호스팅을 제공합니다. <a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a>에서 기여하세요.",
        },
        Form: Form{
            Username:                "사용자 이름",
            Password:                "비밀번호",
            StrictTitleMatching:     "엄격한 제목 매칭",
            StrictTitleMatchingHint: "권장 사항: 영화 또는 시리즈 제목과 정확히 일치하지 않는 결과를 필터링하여 제외합니다",
            PreferredLanguage:       "선호하는 오디오 언어",
            PreferredLanguageHint:   "선호하는 언어로 로컬라이징된 제목을 가진 콘텐츠를 검색하고 우선시하는 데 사용됩니다",
            SortingMethod:           "정렬 방법",
            SortingMethodHint:       "모든 옵션은 동일한 관련성 우선 API 검색을 수행한 다음 결과를 로컬에서 정렬합니다",
            UILanguage:              "UI 언어",
            ShowQualities:           "스트림 목록에 표시할 화질",
            MaxResultsPerQuality:    "화질별 최대 결과 수",
            MaxFileSize:             "최대 파일 크기 (GB)",
            NoLimit:                 "제한 없음",
        },
        Languages: Languages{
            NoPreference: "지정 안 함",
            English:      "영어",
            German:       "독일어 (Deutsch)",
            Spanish:      "스페인어 (Español)",
            French:       "프랑스어 (Français)",
            Italian:      "이탈리아어 (Italiano)",
            Japanese:     "일본어 (日本語)",
            Portuguese:   "포르투갈어 (Português)",
            Russian:      "러시아어 (Русский)",
            Korean:       "한국어",
            Chinese:      "중국어 (中文)",
            Dutch:        "네덜란드어 (Nederlands)",
            Romanian:     "루마니아어 (Română)",
            Bulgarian:    "불가리아어 (Български)",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "화질 우선 (4K → 1080p → 720p)",
            LanguageFirst: "선호 언어 우선, 그 다음 화질",
            SizeFirst:     "파일 크기 우선 (큰 순서대로)",
            DateFirst:     "추가된 날짜 우선 (최신 순서대로)",
        },
        QualityOptions: QualityOptions{AllQualities: "모든 화질"},
        Errors:         Errors{AuthFailed: "인증 실패: 잘못된 사용자 이름 또는 비밀번호\n자격 증명을 확인하고 애드온을 다시 설정하세요"},
    },
    ZH: {
        ConfigPage: ConfigPage{
            Title:        "配置",
            CopyConfig:   "复制配置",
            AddToStremio: "添加到 Stremio",
            ConfigCopied: "已复制！",
            Version:      "版本",
            Description:  "适用于 Stremio 及兼容应用（Omni、Vidi、Fusion、Nuvio）的开源 Easynews 插件。它搜索 Easynews 并返回可播放的流媒体，支持智能标题匹配、画质排序、语言过滤和自我托管。欢迎在 <a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a> 上做出贡献。",
        },
        Form: Form{
            Username:                "用户名",
            Password:                "密码",
            StrictTitleMatching:     "严格标题匹配",
            StrictTitleMatchingHint: "推荐：过滤掉与电影或剧集标题不完全匹配的结果",
            PreferredLanguage:       "首选音频语言",
            PreferredLanguageHint:   "用于在首选语言中查找并优先考虑具有本地化标题的内容",
            SortingMethod:           "排序方式",
            SortingMethodHint:       "所有选项都使用相同的相关性优先的 API 搜索，然后对本地结果进行排序",
            UILanguage:              "界面语言",
            ShowQualities:           "流媒体列表中显示的画质",
            MaxResultsPerQuality:    "单项画质最大结果数",
            MaxFileSize:             "最大文件大小 (GB)",
            NoLimit:                 "不限",
        },
        Languages: Languages{
            NoPreference: "不指定",
            English:      "英语",
            German:       "德语 (Deutsch)",
            Spanish:      "西班牙语 (Español)",
            French:       "法语 (Français)",
            Italian:      "意大利语 (Italiano)",
            Japanese:     "日语 (日本語)",
            Portuguese:   "葡萄牙语 (Português)",
            Russian:      "俄语 (Русский)",
            Korean:       "韩语 (한국어)",
            Chinese:      "中文",
            Dutch:        "荷兰语 (Nederlands)",
            Romanian:     "罗马尼亚语 (Română)",
            Bulgarian:    "保加利亚语 (Български)",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "画质优先 (4K → 1080p → 720p)",
            LanguageFirst: "首选语言优先，然后画质",
            SizeFirst:     "文件大小优先 (由大到小)",
            DateFirst:     "发布时间优先 (由新到旧)",
        },
        QualityOptions: QualityOptions{AllQualities: "所有画质"},
        Errors:         Errors{AuthFailed: "认证失败：无效的用户名或密码\n请检查您的凭据并重新配置插件"},
    },
    NL: {
        ConfigPage: ConfigPage{
            Title:        "Configuratie",
            CopyConfig:   "Configuratie kopiëren",
            AddToStremio: "Toevoegen aan Stremio",
            ConfigCopied: "Gekopieerd!",
            Version:      "Versie",
            Description:  "Open-source Easynews-addon voor Stremio en compatibele apps (Omni, Vidi, Fusion, Nuvio). Het zoekt in Easynews en retourneert afspeelbare streams, met slimme titelovereenstemming, kwaliteitssortering, taalfiltering en self-hosting. Draag bij op <a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a>.",
        },
        Form: Form{
            Username:                "Gebruikersnaam",
            Password:                "Wachtwoord",
            StrictTitleMatching:     "Strikte Titelovereenkomst",
            StrictTitleMatchingHint: "Aanbevolen: Filtert resultaten die niet exact overeenkomen met de film- of serietitel",
            PreferredLanguage:       "Voorkeurstaal Audio",
            PreferredLanguageHint:   "Gebruikt om inhoud met gelokaliseerde titels in de voorkeurstaal te vinden en te prioriteren",
            SortingMethod:           "Sorteermethode",
            SortingMethodHint:       "Alle opties gebruiken dezelfde relevantie-eerst API-zoekopdracht en sorteren de resultaten lokaal",
            UILanguage:              "UI Taal",
            ShowQualities:           "Kwaliteiten om in streamlijst te tonen",
            MaxResultsPerQuality:    "Max resultaten per kwaliteit",
            MaxFileSize:             "Max bestandsgrootte in GB",
            NoLimit:                 "Geen limiet",
        },
        Languages: Languages{
            NoPreference: "Geen voorkeur",
            English:      "Engels",
            German:       "Duits (Deutsch)",
            Spanish:      "Spaans (Español)",
            French:       "Frans (Français)",
            Italian:      "Italiaans (Italiano)",
            Japanese:     "Japans (日本語)",
            Portuguese:   "Portugees (Português)",
            Russian:      "Russisch (Russisch)",
            Korean:       "Koreaans (한국어)",
            Chinese:      "Chinees (中文)",
            Dutch:        "Nederlands",
            Romanian:     "Roemeens (Română)",
            Bulgarian:    "Bulgaars (Български)",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "Kwaliteit (4K → 1080p → 720p)",
            LanguageFirst: "Voorkeurstaal, daarna Kwaliteit",
            SizeFirst:     "Bestandsgrootte (grootste eerst)",
            DateFirst:     "Datum toegevoegd (nieuwste eerst)",
        },
        QualityOptions: QualityOptions{AllQualities: "Alle kwaliteiten"},
        Errors:         Errors{AuthFailed: "Authenticatie mislukt: Ongeldige gebruikersnaam of wachtwoord\nControleer uw inloggegevens en configureer de addon opnieuw"},
    },
    RO: {
        ConfigPage: ConfigPage{
            Title:        "Configurare",
            CopyConfig:   "Copiază configurarea",
            AddToStremio: "Adaugă în Stremio",
            ConfigCopied: "Copiat!",
            Version:      "Versiune",
            Description:  "Addon Easynews open-source pentru Stremio și aplicații compatibile (Omni, Vidi, Fusion, Nuvio). Caută în Easynews și returnează stream-uri redabile, cu potrivire inteligentă a titlurilor, sortare după calitate, filtrare lingvistică și auto-găzduire. Contribuiți pe <a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a>.",
        },
        Form: Form{
            Username:                "Nume utilizator",
            Password:                "Parolă",
            StrictTitleMatching:     "Potrivire strictă a titlului",
            StrictTitleMatchingHint: "Recomandat: Filtrează rezultatele care nu se potrivesc exact cu titlul filmului sau serialului",
            PreferredLanguage:       "Limba audio preferată",
            PreferredLanguageHint:   "Folosit pentru a găsi și prioritiza conținutul cu titluri traduse în limba preferată",
            SortingMethod:           "Metodă de sortare",
            SortingMethodHint:       "Toate opțiunile folosesc aceeași căutare API bazată pe relevanță, apoi sortează rezultatele local",
            UILanguage:              "Limba interfeței",
            ShowQualities:           "Calități de afișat în listă",
            MaxResultsPerQuality:    "Rezultate max per calitate",
            MaxFileSize:             "Dimensiune max fișier în GB",
            NoLimit:                 "Fără limită",
        },
        Languages: Languages{
            NoPreference: "Fără preferință",
            English:      "Engleză",
            German:       "Germană (Deutsch)",
            Spanish:      "Spaniolă (Español)",
            French:       "Franceză (Français)",
            Italian:      "Italiană (Italiano)",
            Japanese:     "Japoneză (日本語)",
            Portuguese:   "Portugheză (Português)",
            Russian:      "Rusă (Русский)",
            Korean:       "Coreeană (한국어)",
            Chinese:      "Chineză (中文)",
            Dutch:        "Olandeză (Nederlands)",
            Romanian:     "Română",
            Bulgarian:    "Bulgară (Български)",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "Calitate (4K → 1080p → 720p)",
            LanguageFirst: "Limba preferată, apoi Calitate",
            SizeFirst:     "Dimensiune fișier (cel mai mare mai întâi)",
            DateFirst:     "Data adăugării (cel mai nou mai întâi)",
        },
        QualityOptions: QualityOptions{AllQualities: "Toate calitățile"},
        Errors:         Errors{AuthFailed: "Autentificare eșuată: Nume utilizator sau parolă incorecte\nVerificați-vă acreditările și reconfigurați addon-ul"},
    },
    BG: {
        ConfigPage: ConfigPage{
            Title:        "Конфигурация",
            CopyConfig:   "Копиране на конфигурацията",
            AddToStremio: "Добавяне към Stremio",
            ConfigCopied: "Копирано!",
            Version:      "Версия",
            Description:  "Easynews добавка с отворен код за Stremio и съвместими приложения (Omni, Vidi, Fusion, Nuvio). Тя търси в Easynews и връща възпроизвеждани потоци, с интелигентно напасване на заглавията, сортиране по качество, филтриране по език и самостоятелно хостване. Допринесете в <a href=\"https://github.com/kiskey/stremio-easynews-go\">GitHub</a>.",
        },
        Form: Form{
            Username:                "Потребителско име",
            Password:                "Парола",
            StrictTitleMatching:     "Строго съвпадение на заглавията",
            StrictTitleMatchingHint: "Препоръчително: Филтрира резултати, които не съвпадат точно с името на филма или сериала",
            PreferredLanguage:       "Предпочитан език за аудио",
            PreferredLanguageHint:   "Използва се за намиране и приоритизиране на съдържание с преведено заглавие на предпочитания език",
            SortingMethod:           "Метод на сортиране",
            SortingMethodHint:       "Всички опции използват едно и също търсене по релевантност през API, след което сортират резултатите локално",
            UILanguage:              "Език на интерфейса",
            ShowQualities:           "Качества за показване в списъка",
            MaxResultsPerQuality:    "Максимум резултати на качество",
            MaxFileSize:             "Максимален размер на файла в GB",
            NoLimit:                 "Без лимит",
        },
        Languages: Languages{
            NoPreference: "Без предпочитание",
            English:      "Английски",
            German:       "Немски (Deutsch)",
            Spanish:      "Испански (Español)",
            French:       "Френски (Français)",
            Italian:      "Италиански (Italiano)",
            Japanese:     "Японски (日本語)",
            Portuguese:   "Португалски (Português)",
            Russian:      "Руски (Русский)",
            Korean:       "Корейски (한국어)",
            Chinese:      "Китайски (中文)",
            Dutch:        "Нидерландски (Nederlands)",
            Romanian:     "Румънски (Română)",
            Bulgarian:    "Български",
        },
        SortingOptions: SortingOptions{
            QualityFirst:  "Качество (4K → 1080p → 720p)",
            LanguageFirst: "Предпочитан език, след това Качество",
            SizeFirst:     "Размер на файла (най-големите първи)",
            DateFirst:     "Дата на добавяне (най-новите първи)",
        },
        QualityOptions: QualityOptions{AllQualities: "Всички качества"},
        Errors:         Errors{AuthFailed: "Неуспешна идентификация: Невалидно потребителско име или парола\nПроверете данните си за вход и пренастройте добавката"},
    },
}

// ConvertToTMDBLanguageCode converts a 3-letter UI language code to a 2-letter TMDB language code.
func ConvertToTMDBLanguageCode(langCode string) string {
    if t, ok := ISOToLanguage[langCode]; ok {
        return iso1Map[t]
    }
    if code, ok := AdditionalLanguageCodes[langCode]; ok {
        return code
    }
    return langCode
}

// ConvertToISO6392 maps a 2-letter ISO 639-1 language code back to our 3-letter codes.
func ConvertToISO6392(iso1 string) string {
    if v, ok := iso1ToISO3Map[strings.ToLower(iso1)]; ok {
        return v
    }
    return iso1
}
