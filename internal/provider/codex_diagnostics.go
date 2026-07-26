package provider

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
)

const (
	maxCodexDiagnosticBytes          = 4 * 1024
	maxCodexDiagnosticFrames         = 64
	maxCodexDiagnosticInspectedBytes = 256 * 1024
	maxCodexDiagnosticFrameBytes     = 64 * 1024
)

type codexFailureCategory uint8

const (
	codexFailureGeneric codexFailureCategory = iota
	codexFailureAuthentication
	codexFailureModelAccess
	codexFailureQuota
	codexFailureRateLimit
	codexFailureNetwork
	codexFailureService
	codexFailureInvalidRequest
	codexFailureCategoryCount
)

func (c codexFailureCategory) String() string {
	switch c {
	case codexFailureAuthentication:
		return "provider authentication rejected"
	case codexFailureModelAccess:
		return "provider model unavailable or access denied"
	case codexFailureQuota:
		return "provider quota exhausted"
	case codexFailureRateLimit:
		return "provider rate limited"
	case codexFailureNetwork:
		return "provider network or timeout failure"
	case codexFailureService:
		return "provider service unavailable"
	case codexFailureInvalidRequest:
		return "provider invalid request"
	default:
		return "provider failure"
	}
}

type codexDiagnosticEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
}

// codexDiagnosticCollector retains only fixed category constants. It never
// stores raw JSONL frames, provider messages, or non-error event payloads.
type codexDiagnosticCollector struct {
	categories      []codexFailureCategory
	seen            map[codexFailureCategory]struct{}
	framesInspected int
	bytesInspected  int
	full            bool
	exhausted       bool
	turnFailed      bool
	oversizeSchema  bool
}

func (c *codexDiagnosticCollector) consume(line []byte) {
	frame := codexFrameWithoutRecordDelimiter(line)
	inspection := inspectCodexTopLevelType(frame)
	if inspection.duplicate || inspection.reordered {
		c.oversizeSchema = true
		c.failClosed()
		return
	}
	// Framing inspection does not copy provider keys or arbitrary values from
	// the already retained record. It keeps authoritative turn.failed
	// recognition independent of the much smaller diagnostic-message budget.
	if inspection.kind == "turn.failed" {
		c.turnFailed = true
	}
	if len(frame) > maxCodexDiagnosticFrameBytes {
		if !c.full && (inspection.kind == "error" || c.turnFailed) {
			c.failClosed()
		}
		return
	}

	var event codexDiagnosticEvent
	if err := json.Unmarshal(frame, &event); err != nil {
		return
	}
	if c.full {
		return
	}
	if c.framesInspected >= maxCodexDiagnosticFrames ||
		len(frame) > maxCodexDiagnosticInspectedBytes-c.bytesInspected {
		c.failClosed()
		return
	}
	c.framesInspected++
	c.bytesInspected += len(frame)

	var message string
	switch event.Type {
	case "error":
		message = event.Message
	case "turn.failed":
		message = event.Error.Message
	default:
		return
	}
	if message == "" {
		return
	}
	c.add(classifyCodexFailure(message))
}

func (c *codexDiagnosticCollector) failed() bool {
	return c.failure() != nil
}

func (c *codexDiagnosticCollector) failure() error {
	if c.turnFailed {
		return ErrCodexTurnFailed
	}
	if c.oversizeSchema {
		return ErrCodexOversizeSchema
	}
	return nil
}

// consumeDiscardedPrefix classifies only a bounded prefix of an oversized
// record. An immediate turn.failed remains authoritative. Every other
// structured or inconclusive record fails closed because a later duplicate or
// reordered discriminator cannot be ruled out without the complete record.
// Only a prefix that positively proves a non-object remains pane noise.
func (c *codexDiagnosticCollector) consumeDiscardedPrefix(prefix []byte) bool {
	kind, object, typeFirst := codexTopLevelTypeFromPrefix(prefix)
	if !object {
		return false
	}
	if typeFirst && kind == "turn.failed" {
		c.turnFailed = true
		c.failClosed()
		return true
	}
	c.oversizeSchema = true
	c.failClosed()
	return true
}

func codexTopLevelTypeFromPrefix(prefix []byte) (kind string, object, typeFirst bool) {
	first, found := firstNonJSONWhitespace(prefix)
	if !found {
		// Whitespace alone is inconclusive: the opening object delimiter may be
		// the next discarded byte.
		return "", true, false
	}
	if prefix[first] != '{' {
		return "", false, false
	}
	decoder := json.NewDecoder(bytes.NewReader(prefix[first:]))
	token, err := decoder.Token()
	if err != nil {
		return "", true, false
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return "", true, false
	}
	keyToken, err := decoder.Token()
	if err != nil {
		// The opening object delimiter is already proven. An incomplete or
		// over-prefix first key is structured-but-unclassifiable and must fail
		// closed rather than hide a later reordered type discriminator.
		return "", true, false
	}
	key, ok := keyToken.(string)
	if !ok {
		return "", true, false
	}
	if key != "type" {
		return "", true, false
	}
	valueToken, err := decoder.Token()
	if err != nil {
		return "", true, false
	}
	value, ok := valueToken.(string)
	if !ok {
		return "", true, false
	}
	return value, true, true
}

type codexTopLevelTypeInspection struct {
	kind      string
	keys      int
	typeKeys  int
	duplicate bool
	reordered bool
}

func codexFrameWithoutRecordDelimiter(line []byte) []byte {
	if len(line) > 0 && line[len(line)-1] == '\n' {
		return line[:len(line)-1]
	}
	return line
}

// inspectCodexTopLevelType validates the complete retained frame, then walks
// only its syntax bytes. It never copies or decodes arbitrary values, so a
// multi-MiB command payload does not create a second provider-controlled
// allocation.
func inspectCodexTopLevelType(frame []byte) codexTopLevelTypeInspection {
	first, found := firstNonJSONWhitespace(frame)
	if !found || frame[first] != '{' {
		return codexTopLevelTypeInspection{}
	}
	if !json.Valid(frame) {
		return codexTopLevelTypeInspection{}
	}

	var inspection codexTopLevelTypeInspection
	depth := 1
	expectKey := true
	for i := first + 1; i < len(frame) && depth > 0; {
		switch frame[i] {
		case '"':
			end := jsonStringEnd(frame, i)
			if depth == 1 && expectKey {
				inspection.keys++
				if jsonStringEqualASCII(frame[i:end], "type") {
					inspection.typeKeys++
					if inspection.typeKeys > 1 {
						inspection.duplicate = true
					} else {
						inspection.reordered = inspection.keys != 1
						inspection.kind = codexTopLevelStringValue(frame, end)
					}
				}
				expectKey = false
			}
			i = end
		case '{', '[':
			depth++
			i++
		case '}', ']':
			depth--
			i++
		case ',':
			if depth == 1 {
				expectKey = true
			}
			i++
		default:
			i++
		}
	}
	return inspection
}

func codexTopLevelStringValue(frame []byte, keyEnd int) string {
	i := keyEnd
	for i < len(frame) && isJSONWhitespace(frame[i]) {
		i++
	}
	if i >= len(frame) || frame[i] != ':' {
		return ""
	}
	i++
	for i < len(frame) && isJSONWhitespace(frame[i]) {
		i++
	}
	if i >= len(frame) || frame[i] != '"' {
		return ""
	}
	end := jsonStringEnd(frame, i)
	value := frame[i:end]
	switch {
	case jsonStringEqualASCII(value, "turn.failed"):
		return "turn.failed"
	case jsonStringEqualASCII(value, "error"):
		return "error"
	default:
		return ""
	}
}

func jsonStringEqualASCII(encoded []byte, want string) bool {
	if len(encoded) < 2 || encoded[0] != '"' || encoded[len(encoded)-1] != '"' {
		return false
	}
	wantAt := 0
	for i := 1; i < len(encoded)-1; {
		var decoded byte
		if encoded[i] != '\\' {
			if encoded[i] >= unicode.MaxASCII {
				return false
			}
			decoded = encoded[i]
			i++
		} else {
			i++
			if i >= len(encoded)-1 {
				return false
			}
			switch encoded[i] {
			case '"', '\\', '/':
				decoded = encoded[i]
				i++
			case 'b':
				decoded = '\b'
				i++
			case 'f':
				decoded = '\f'
				i++
			case 'n':
				decoded = '\n'
				i++
			case 'r':
				decoded = '\r'
				i++
			case 't':
				decoded = '\t'
				i++
			case 'u':
				if i+4 >= len(encoded)-1 {
					return false
				}
				code := 0
				for _, digit := range encoded[i+1 : i+5] {
					value, ok := jsonHexValue(digit)
					if !ok {
						return false
					}
					code = code<<4 | value
				}
				if code >= unicode.MaxASCII {
					return false
				}
				decoded = byte(code)
				i += 5
			default:
				return false
			}
		}
		if wantAt >= len(want) || decoded != want[wantAt] {
			return false
		}
		wantAt++
	}
	return wantAt == len(want)
}

func jsonHexValue(value byte) (int, bool) {
	switch {
	case value >= '0' && value <= '9':
		return int(value - '0'), true
	case value >= 'a' && value <= 'f':
		return int(value-'a') + 10, true
	case value >= 'A' && value <= 'F':
		return int(value-'A') + 10, true
	default:
		return 0, false
	}
}

func jsonStringEnd(frame []byte, start int) int {
	for i := start + 1; i < len(frame); i++ {
		switch frame[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(frame)
}

func firstNonJSONWhitespace(value []byte) (int, bool) {
	for i, b := range value {
		if !isJSONWhitespace(b) {
			return i, true
		}
	}
	return 0, false
}

func isJSONWhitespace(value byte) bool {
	switch value {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}

func (c *codexDiagnosticCollector) add(category codexFailureCategory) {
	if c.full {
		return
	}
	if category >= codexFailureCategoryCount {
		category = codexFailureGeneric
	}
	if c.seen == nil {
		c.seen = make(map[codexFailureCategory]struct{})
	}
	if _, duplicate := c.seen[category]; duplicate {
		return
	}
	c.seen[category] = struct{}{}
	c.categories = append(c.categories, category)
	c.full = len(c.seen) == int(codexFailureCategoryCount)
}

func (c *codexDiagnosticCollector) failClosed() {
	c.categories = []codexFailureCategory{codexFailureGeneric}
	c.seen = map[codexFailureCategory]struct{}{codexFailureGeneric: {}}
	c.full = true
	c.exhausted = true
}

func (c *codexDiagnosticCollector) String() string {
	if len(c.categories) == 0 {
		return codexFailureGeneric.String()
	}
	parts := make([]string, 0, len(c.categories))
	for _, category := range c.categories {
		parts = append(parts, category.String())
	}
	diagnostic := strings.Join(parts, "; ")
	if len(diagnostic) > maxCodexDiagnosticBytes {
		// All inputs above are fixed constants and currently total far less than
		// this bound. Fail closed if that contract is ever expanded carelessly.
		return codexFailureGeneric.String()
	}
	return diagnostic
}

// classifyCodexFailure inspects provider text transiently and returns only a
// fixed constant. No capture or substring from message crosses this boundary.
func classifyCodexFailure(message string) codexFailureCategory {
	message = normalizeCodexDiagnostic(message)
	// This order is intentional and tested. It resolves overlapping provider
	// prose deterministically while exact token/phrase matching prevents
	// fragments such as "modeling", "socketed", or "5030" from becoming false
	// classifications.
	switch {
	case containsAnyDiagnosticPhrase(message,
		"authentication", "unauthorized", "not authenticated", "not logged in",
		"log in", "login", "log out", "sign in again", "api key", "api_key",
		"credential", "access token could not be refreshed",
		"refresh token has expired", "401",
	):
		return codexFailureAuthentication
	case containsAnyDiagnosticPhrase(message, "rate limit", "rate_limit", "too many requests", "429"):
		return codexFailureRateLimit
	case containsAnyDiagnosticPhrase(message,
		"quota", "insufficient_quota", "billing limit", "usage limit",
		"credits exhausted", "out of credits", "spend cap",
	):
		return codexFailureQuota
	case containsAnyDiagnosticPhrase(message, "context window", "ran out of room"):
		return codexFailureInvalidRequest
	case containsAnyDiagnosticPhrase(message,
		"service unavailable", "temporarily unavailable", "internal server error",
		"overloaded", "at capacity", "high demand", "temporary errors",
		"bad gateway", "gateway timeout", "500", "502", "503", "504",
	):
		return codexFailureService
	case containsAnyDiagnosticPhrase(message,
		"model", "access denied", "permission denied", "forbidden", "403",
	):
		return codexFailureModelAccess
	case containsAnyDiagnosticPhrase(message,
		"network", "timeout", "timed out", "connection", "dns", "tls",
		"socket", "unreachable",
	):
		return codexFailureNetwork
	case containsAnyDiagnosticPhrase(message,
		"invalid request", "bad request", "malformed", "validation",
		"input too large", "unsupported", "400",
	):
		return codexFailureInvalidRequest
	default:
		return codexFailureGeneric
	}
}

func normalizeCodexDiagnostic(message string) string {
	var normalized strings.Builder
	normalized.Grow(len(message) + 2)
	normalized.WriteByte(' ')
	separated := true
	for _, r := range strings.ToLower(message) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			normalized.WriteRune(r)
			separated = false
			continue
		}
		if !separated {
			normalized.WriteByte(' ')
			separated = true
		}
	}
	if !separated {
		normalized.WriteByte(' ')
	}
	return normalized.String()
}

func containsAnyDiagnosticPhrase(message string, phrases ...string) bool {
	for _, phrase := range phrases {
		normalizedPhrase := strings.TrimSpace(normalizeCodexDiagnostic(phrase))
		if normalizedPhrase != "" &&
			strings.Contains(message, " "+normalizedPhrase+" ") {
			return true
		}
	}
	return false
}
