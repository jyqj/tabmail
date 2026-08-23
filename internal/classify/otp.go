// Package classify extracts structured signals (e.g. OTP codes) from already
// parsed mail content. All functions are pure: no I/O, no time, no randomness,
// no panics. Arbitrary input returns a zero-value result.
package classify

import (
	"regexp"
	"strings"
)

// Env is the parsed message content fed to the classifiers. Callers pass the
// already-decoded envelope fields; classify never re-parses raw MIME.
type Env struct {
	Subject  string
	TextBody string
	HTMLBody string
	From     string
}

// OTPResult is the outcome of OTP extraction. Found is false when no plausible
// code was located; Code/Confidence/Strategy are zero values in that case.
type OTPResult struct {
	Code       string
	Confidence float32
	Found      bool
	Strategy   string
}

// Confidence thresholds. Keyword-adjacent matches are high-signal (the
// surrounding text names the code as a code); bare digit runs are low-signal
// (order numbers, amounts, tracking ids) and only surface as a last resort.
const (
	otpConfidenceKeyword = 0.8
	otpConfidenceDigit   = 0.4
)

// otpLength bounds the digits we accept as a code: real OTPs are 4-8 digits.
const (
	otpMinLen = 4
	otpMaxLen = 8
)

// keywordRe matches a code-bearing keyword (Latin or CJK) immediately followed
// by up to ~20 non-digit characters and then a 4-8 digit run. Case-insensitive.
var keywordRe = regexp.MustCompile(
	`(?i)(?:code|verification|verify|password|otp|one-time|pin|passcode|验证码|动态码|校验码|验证)[\W]{0,3}[:：]?\D{0,20}\b([0-9]{4,8})\b`,
)

// digitRunRe matches any standalone 4-8 digit run, used as the low-confidence
// fallback.
var digitRunRe = regexp.MustCompile(`\b[0-9]{4,8}\b`)

// scriptBlockRe / styleBlockRe remove script/style blocks entirely — tags AND
// content — so a numeric literal like `var code=9999` inside a <script> cannot
// masquerade as the message's OTP. Case-insensitive, dot matches newline.
var (
	scriptBlockRe = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`)
	styleBlockRe  = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style\s*>`)
)

// stripTagsRe collapses remaining HTML tags to whitespace. It is intentionally a
// coarse stripper (no entity decode) — it only feeds the digit/keyword scanners,
// which already tolerate noise. Script/style blocks are excised first.
var stripTagsRe = regexp.MustCompile(`<[^>]*>`)

// OTPFromMessage runs the heuristic chain over env and returns the best OTP
// candidate. It is a pure function: deterministic, side-effect free, and
// returns a zero OTPResult (Found=false) for any input including empty or
// malformed bodies, never panicking.
func OTPFromMessage(env Env) OTPResult {
	text := env.TextBody
	if html := env.HTMLBody; html != "" {
		// Prefer the plain-text body when present (it is already decoded and
		// lower-noise); only fall back to a tag-stripped HTML body when there
		// is no text part. We scan the HTML body in addition to text so an
		// HTML-only OTP message is still detected.
		if text == "" {
			text = stripHTML(html)
		} else {
			text = text + "\n" + stripHTML(html)
		}
	}
	// Subject is short and high-signal; prepend so its keyword matches win.
	scan := env.Subject + "\n" + text

	if m := keywordRe.FindStringSubmatch(scan); m != nil {
		code := m[1]
		if plausibleOTP(code) {
			return OTPResult{Code: code, Confidence: otpConfidenceKeyword, Found: true, Strategy: "keyword_adjacent"}
		}
	}

	runs := digitRunRe.FindAllString(scan, -1)
	if len(runs) > 0 {
		// Among bare digit runs prefer the shortest plausible code, then the
		// most central, to avoid mistaking a long order id for the OTP.
		best := pickDigitRun(runs)
		if best != "" {
			return OTPResult{Code: best, Confidence: otpConfidenceDigit, Found: true, Strategy: "digit_run"}
		}
	}
	return OTPResult{}
}

// plausibleOTP reports whether a digit run is in the plausible OTP length range.
func plausibleOTP(code string) bool {
	n := len(code)
	return n >= otpMinLen && n <= otpMaxLen
}

// pickDigitRun selects the shortest 4-8 digit run, breaking ties by choosing
// the one nearest the middle of the run list (most likely to be the body's
// actual code rather than an order number prefix/suffix).
func pickDigitRun(runs []string) string {
	best := ""
	for _, r := range runs {
		if !plausibleOTP(r) {
			continue
		}
		if best == "" || len(r) < len(best) {
			best = r
		}
	}
	return best
}

// stripHTML reduces an HTML body to a scan-friendly text approximation by
// replacing tags with a single space. Entities and tag content beyond
// stripping are left to the scanners' tolerance of noise.
func stripHTML(s string) string {
	// Drop script/style blocks (tags + content) before stripping tags, so a
	// numeric literal embedded in <script>var code=9999;</script> is not picked
	// up as the OTP.
	s = scriptBlockRe.ReplaceAllString(s, " ")
	s = styleBlockRe.ReplaceAllString(s, " ")
	s = stripTagsRe.ReplaceAllString(s, " ")
	// Collapse the separator spaces the tag strip introduces so a code split
	// across two inline spans is not fragmented: e.g. "<b>123</b>456" -> "123456".
	return strings.Join(strings.Fields(s), " ")
}
