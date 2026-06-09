package shared

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// Base Logger Interface
// ---------------------------------------------------------------------------

type Logger interface {
	Error(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Debug(msg string, args ...interface{})
	Silly(msg string, args ...interface{})
	GetLevel() string
}

// ---------------------------------------------------------------------------
// Zerolog-based implementation (production / server)
// ---------------------------------------------------------------------------

type ZerologLogger struct {
	logger zerolog.Logger
	level  string
}

func NewZerologLogger(prefix string, level string) *ZerologLogger {
	lvl, _ := zerolog.ParseLevel(level)
	if lvl == zerolog.NoLevel {
		lvl = zerolog.InfoLevel
	}
	output := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
		FormatMessage: func(i interface{}) string {
			return fmt.Sprintf("[v%s] %s", GetVersion(), i)
		},
	}
	l := zerolog.New(output).Level(lvl).With().Timestamp().Logger()
	if prefix != "" {
		l = l.With().Str("prefix", prefix).Logger()
	}
	return &ZerologLogger{logger: l, level: lvl.String()}
}

func (z *ZerologLogger) Error(msg string, args ...interface{}) {
	z.logger.Error().Msgf(msg, args...)
}
func (z *ZerologLogger) Warn(msg string, args ...interface{}) {
	z.logger.Warn().Msgf(msg, args...)
}
func (z *ZerologLogger) Info(msg string, args ...interface{}) {
	z.logger.Info().Msgf(msg, args...)
}
func (z *ZerologLogger) Debug(msg string, args ...interface{}) {
	z.logger.Debug().Msgf(msg, args...)
}
func (z *ZerologLogger) Silly(msg string, args ...interface{}) {
	z.logger.Trace().Msgf(msg, args...)
}
func (z *ZerologLogger) GetLevel() string { return z.level }

// ---------------------------------------------------------------------------
// SummaryLogger: deduplicates noisy debug patterns (Audited and thread-safe)
// ---------------------------------------------------------------------------

type summaryCounter struct {
	count     int64 // Read and written atomically
	firstMsg  string
	firstArgs []interface{}
	mu        sync.Mutex // Protects firstMsg and firstArgs from concurrent access
}

type SummaryLogger struct {
	base       Logger
	counters   sync.Map // map[string]*summaryCounter
	patterns   []*regexp.Regexp
	ticker     *time.Ticker
	stopCh     chan struct{}
	stopOnce   sync.Once // Prevents channel panics on duplicate Stop() calls
	flushDelay time.Duration
}

// Pre-compiled patterns matching the original SummaryLogger logic.
var summaryPatterns = []*regexp.Regexp{
	regexp.MustCompile(`Checking if video is bad: "([^"]+)"`),
	regexp.MustCompile(`Bad video: "([^"]+)": (.+)`),
	regexp.MustCompile(`Video passed quality checks: "([^"]+)"`),
	regexp.MustCompile(`Matching title: "([^"]+)" against query: "([^"]+)"`),
	regexp.MustCompile(`Found (\d+) results for "([^"]+)"`),
	regexp.MustCompile(`Rejected (.+) by title matching: "([^"]+)"`),
	regexp.MustCompile(`Total unique results so far: (\d+)`),
	regexp.MustCompile(`Cache (hit|miss|expired) for key:`),
	regexp.MustCompile(`Mapping stream: "([^"]+)"`),
	regexp.MustCompile(`Stream "([^"]+)" has languages:`),
	regexp.MustCompile(`Creating stream URL with farm:`),
	regexp.MustCompile(`Received \w+ request for:`),
	regexp.MustCompile(`After quality filtering: (\d+) streams remain`),
	regexp.MustCompile(`Quality \w+: (\d+) streams`),
	regexp.MustCompile(`Reached global limit of (\d+) streams`),
}

var (
	genericQuoteRe = regexp.MustCompile(`".+?"`)
	genericDigitRe = regexp.MustCompile(`\d+`)
)

func NewSummaryLogger(base Logger) *SummaryLogger {
	sl := &SummaryLogger{
		base:       base,
		patterns:   summaryPatterns,
		flushDelay: 1 * time.Second,
		stopCh:     make(chan struct{}),
	}
	if os.Getenv("CLOUDFLARE") != "true" {
		sl.ticker = time.NewTicker(sl.flushDelay)
		go sl.flusher()
	}
	return sl
}

func (sl *SummaryLogger) flusher() {
	for {
		select {
		case <-sl.ticker.C:
			sl.flush()
		case <-sl.stopCh:
			return
		}
	}
}

func (sl *SummaryLogger) flush() {
	sl.counters.Range(func(key, value interface{}) bool {
		sc := value.(*summaryCounter)
		count := atomic.LoadInt64(&sc.count)
		if count > 1 {
			sl.base.Debug("[SUMMARY] %s: %d similar logs", key.(string), count)
		} else if count == 1 {
			sc.mu.Lock()
			msg := sc.firstMsg
			args := sc.firstArgs
			sc.mu.Unlock()
			sl.base.Debug(msg, args...)
		}
		return true
	})
	sl.counters = sync.Map{}
}

func (sl *SummaryLogger) shouldSummarize(msg string) bool {
	for _, p := range sl.patterns {
		if p.MatchString(msg) {
			return true
		}
	}
	return false
}

func (sl *SummaryLogger) getPattern(msg string) string {
	for _, p := range sl.patterns {
		if m := p.FindStringSubmatch(msg); m != nil {
			category := "unknown"
			if len(m) > 1 {
				category = m[1]
				if len(category) > 50 {
					category = category[:50]
				}
			}
			patternStr := p.String()
			shortPattern := patternStr
			if len(shortPattern) > 40 {
				shortPattern = shortPattern[:40]
			}
			return fmt.Sprintf("%s >> %s", shortPattern, category)
		}
	}

	generic := genericQuoteRe.ReplaceAllString(msg, `"..."`)
	generic = genericDigitRe.ReplaceAllString(generic, "#")
	if len(generic) > 60 {
		generic = generic[:60]
	}
	return "Generic: " + generic
}

func (sl *SummaryLogger) Error(msg string, args ...interface{}) { sl.base.Error(msg, args...) }
func (sl *SummaryLogger) Warn(msg string, args ...interface{})  { sl.base.Warn(msg, args...) }
func (sl *SummaryLogger) Info(msg string, args ...interface{})  { sl.base.Info(msg, args...) }
func (sl *SummaryLogger) Silly(msg string, args ...interface{}) { sl.base.Silly(msg, args...) }

func (sl *SummaryLogger) Debug(msg string, args ...interface{}) {
	if sl.shouldSummarize(msg) {
		pattern := sl.getPattern(msg)
		actual, _ := sl.counters.LoadOrStore(pattern, &summaryCounter{})
		sc := actual.(*summaryCounter)
		
		newCount := atomic.AddInt64(&sc.count, 1)
		if newCount == 1 {
			sc.mu.Lock()
			sc.firstMsg = msg
			sc.firstArgs = args
			sc.mu.Unlock()
		}
	} else {
		sl.base.Debug(msg, args...)
	}
}

func (sl *SummaryLogger) GetLevel() string { return sl.base.GetLevel() }

func (sl *SummaryLogger) Stop() {
	sl.stopOnce.Do(func() {
		if sl.ticker != nil {
			sl.ticker.Stop()
		}
		close(sl.stopCh)
		sl.flush()
	})
}

// ---------------------------------------------------------------------------
// CloudflareLogger: simple console logger for Workers (no background tickers)
// ---------------------------------------------------------------------------

type CloudflareLogger struct {
	level string
}

func NewCloudflareLogger(level string) *CloudflareLogger {
	return &CloudflareLogger{level: strings.ToLower(level)}
}

func (c *CloudflareLogger) shouldLog(lvl string) bool {
	levels := map[string]int{"error": 0, "warn": 1, "info": 2, "debug": 3, "silly": 4}
	configLevel, ok := levels[c.level]
	if !ok {
		configLevel = 2 // info default
	}
	msgLevel, ok := levels[lvl]
	if !ok {
		return true
	}
	return msgLevel <= configLevel
}

func (c *CloudflareLogger) log(level string, msg string, args ...interface{}) {
	if !c.shouldLog(level) {
		return
	}
	formatted := fmt.Sprintf("[v%s] %s: %s", GetVersion(), strings.ToUpper(level), fmt.Sprintf(msg, args...))
	if level == "error" {
		fmt.Fprintln(os.Stderr, formatted)
	} else {
		fmt.Println(formatted)
	}
}

func (c *CloudflareLogger) Error(msg string, args ...interface{}) { c.log("error", msg, args...) }
func (c *CloudflareLogger) Warn(msg string, args ...interface{})  { c.log("warn", msg, args...) }
func (c *CloudflareLogger) Info(msg string, args ...interface{})  { c.log("info", msg, args...) }
func (c *CloudflareLogger) Debug(msg string, args ...interface{}) { c.log("debug", msg, args...) }
func (c *CloudflareLogger) Silly(msg string, args ...interface{}) { c.log("silly", msg, args...) }
func (c *CloudflareLogger) GetLevel() string                      { return c.level }

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

func CreateLogger(prefix string, level string) Logger {
	if level == "" {
		level = os.Getenv("EASYNEWS_LOG_LEVEL")
	}
	if level == "" {
		level = "info"
	}

	isCF := os.Getenv("CLOUDFLARE") == "true"

	var base Logger
	if isCF {
		base = NewCloudflareLogger(level)
	} else {
		base = NewZerologLogger(prefix, level)
	}

	if os.Getenv("EASYNEWS_SUMMARIZE_LOGS") != "false" && !isCF {
		return NewSummaryLogger(base)
	}
	return base
}
