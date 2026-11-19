// Package payloadguard is the Record's append guard (A184, A154, REQ-AUD-003, REQ-SEC-003): a string in an event payload never holds a run of more than four digits,
// unless the whole string is a typed ID of the contract, an RFC 3339 timestamp, a content hash or a money string (A279/A270, below). A number-typed field is a number, not a string, and is not
// examined. The limit is four, as on every outward surface (AGENTS.md section 7: mask to the last four digits); a Record that never holds a longer run needs no
// masking of its own, so the hashed `events` array can be served as stored.
//
// The guard reads the canonical JSON of the payload, so it sees exactly what is hashed and stored. A violation names the JSON path and never the value
// (the value may be account data).
//
// **Where it is enforced: in the application only.** `chain.Appender.Append` calls it before anything is signed or written. The database does NOT enforce it: `record_insert_t`
// checks the signature and the chain, and a login that holds EXECUTE on it can store any payload it can sign. The core's own code is the only caller of that function, so the
// guard closes every path of the core, not a compromised or hand-written one (the same limit as the payload itself: the database does not read what it stores). Whether the database
// must enforce it as well is the Architect's call (noema#536); until then nothing in this repository claims that it does.
package payloadguard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Limit is the longest run of digits a payload string may hold.
const Limit = 4

// Mask keeps only the last Limit digits of every run of digits in s and replaces the others with '*' (REQ-AUD-003 "Masked", AGENTS section 7: mask to the last four). A digit is any
// Unicode Nd character, so full-width, Arabic-Indic and Devanagari runs, and runs that mix scripts, are masked like ASCII ones (A154); the separators stay where they are. A run is
// found with digitRun, the same definition the guard refuses by (A194, A231): there is one, with no second mode. Mask has no path, so it has no path-based exemption: a transport class that holds
// digits shows partly masked in a served fact (the guard accepts it only at its two receipt paths, exempt below). It has the money exemption, because that one is by the whole string and not by path
// (A308): a stored `NGN 12,345.00` is served as it is, not `NGN 12,*45.00`. Masking is idempotent. record/reconstruct serves its facts
// through this function (A194).
func Mask(s string) string {
	if isMoney(s) {
		return s
	}
	return digitRun.ReplaceAllStringFunc(s, func(run string) string {
		total := 0
		for _, r := range run {
			if unicode.IsDigit(r) {
				total++
			}
		}
		if total <= Limit {
			return run
		}
		hide := total - Limit // digits to hide
		var out strings.Builder
		for _, r := range run {
			if unicode.IsDigit(r) && hide > 0 {
				out.WriteRune('*')
				hide--
				continue
			}
			out.WriteRune(r)
		}
		return out.String()
	})
}

// ErrDigitRun is wrapped by every violation.
var ErrDigitRun = errors.New("payloadguard: a payload string holds a run of more than four digits")

// separators is the one set of characters that join the digits of a number in this package (A194, A231): every kind of space, including the vertical tab, the four ASCII information separators U+001C to U+001F (A308: the file, group, record and unit separators; the redactor's class already joined them; the
// UI scan's now does), the next-line character
// and the line and paragraph separators (\p{Z}); every dash (\p{Pd}) and the minus sign; every bracket (\p{Ps}, \p{Pe}); the dot, the slash, the colon and the underscore, with their
// full-width forms and the middle dots; every Unicode format character (soft hyphen, zero-width characters, joiners) and every combining mark. "4111 1111 1111 1111",
// "4111-1111-1111-1111", "4111_1111_1111_1111" and "(4111)(1111)(1111)(1111)" are each one run of 16. A comma is not in it, so an amount such as "20,000,000" is not one run. The guard
// and the display mask use this set and no other; the earlier guard left out the underscore, the colon and the brackets so that a code such as VERSION_CONFLICT_409_412 was not
// refused, and A231 replaced that with the one exemption of isReasonCode.
const separators = `\s\x0b\x{1c}-\x{1f}\x{85}\p{Z}\p{Cf}\p{M}\p{Pd}\p{Ps}\p{Pe}.\-/_:\x{2212}\x{b7}\x{30fb}\x{ff0e}\x{ff0f}\x{ff1a}\x{ff3f}`

// digitRun is a run of digits in any script joined by the separators. It is the one definition: Check refuses by it and Mask masks by it.
var digitRun = regexp.MustCompile(`\p{Nd}(?:[` + separators + `]*\p{Nd})*`)

// The one exemption of this rule besides the typed values below (A231, interim; narrowed by the Security Reviewer's and Codex's P1 on noema#600): the transport class a receipt carries.
// The Record's receipt event stores `transport_result` (one transport class of the adapter, a finite vocabulary) and `raw_status`, which is that class, then a colon and a raw code
// when there is one (execution/ledger/complete.go; outcome/receiptsig). One class, VERSION_CONFLICT_409_412, holds six digits, which the guard would otherwise refuse: the receipt
// of every version conflict, after the write was dispatched. The exemption is **at those two exact paths of the payload and for those exact values only**: `/transport_result` must
// be a class of transportClasses, `/raw_status` a class followed by at most maxRawCodeParts parts, each one to three digits or an UPPER_SNAKE word with no digit, and no more than
// maxRawStatusDigits digits in all. The same string at any other path is refused, whatever its shape (a nine-digit identifier cannot be carried as a "code"). A deviation from A231 as
// written (a whole-string UPPER_SNAKE shape anywhere), in the safer direction: the Architect is asked to confirm it on the PR.
const (
	maxRawCodeParts    = 2
	maxRawStatusDigits = 9
	maxRawCodeDigits   = 3

	pathTransportResult = "/transport_result"
	pathRawStatus       = "/raw_status"
)

// transportClasses is the finite vocabulary of execution/controller TransportClass (PERD 27.1, E10); guard_test.go reads that file and fails when the two disagree. record/ imports
// no execution package, so the list is kept here.
var transportClasses = map[string]bool{
	"ACK_2XX": true, "VERSION_CONFLICT_409_412": true, "DUPLICATE_409_SAME_KEY": true, "DUPLICATE_CREATE": true, "REJECTED_4XX": true,
	"TIMEOUT": true, "CONNECTION_RESET": true, "SERVER_5XX": true, "NO_RESPONSE": true, "ADAPTER_NON_RETRYABLE_DEFECT": true,
}

// isReceiptTransportValue reports whether s is exempt at path: the class itself at /transport_result, or the class and its raw-code parts at /raw_status.
func isReceiptTransportValue(path, s string) bool {
	switch path {
	case pathTransportResult:
		return transportClasses[s]
	case pathRawStatus:
		parts := strings.Split(s, ":")
		if len(parts) > 1+maxRawCodeParts || !transportClasses[parts[0]] {
			return false
		}
		total := countDigits(parts[0])
		for _, p := range parts[1:] {
			switch {
			case p != "" && strings.Trim(p, "0123456789") == "":
				if len(p) > maxRawCodeDigits {
					return false
				}
				total += len(p)
			case isUpperSnakeWord(p):
			default:
				return false
			}
		}
		return total <= maxRawStatusDigits
	}
	return false
}

func countDigits(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			n++
		}
	}
	return n
}

// isUpperSnakeWord is a raw code that is a word: capital letters and single underscores, no digit (PRECONDITION_FAILED).
func isUpperSnakeWord(s string) bool {
	if s == "" || s[0] == '_' || s[len(s)-1] == '_' || strings.Contains(s, "__") {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= 'A' && c <= 'Z') && c != '_' {
			return false
		}
	}
	return true
}

// keyID is the shape of the key ID of a signature (`<identity>.<algorithm>.<version>`, such as core-execution.ed25519.1): the algorithm name holds a run of more than four
// digits ("ed25519"). It is exempt at a path that ends in /key_id only, and only in this shape, so the name cannot be put inside another string to split a number.
var keyID = regexp.MustCompile(`\A[a-z][a-z0-9-]*\.(?:ed25519|x25519)\.[0-9]{1,4}\z`)

// typedIDPrefixes are the prefixes of the typed IDs of contracts/schemas/common.v1.json (`<prefix>_` and 2 to 32 letters or digits). guard_test.go reads the contract and fails
// when this list and the contract's ID definitions disagree. `idk` is not here: since noema#564 the contract's IdempotencyKeyId is `idk_v1_` and 64 hex, which is matched whole below, so
// a short `idk_<letters or digits>` is no ID and gets no exemption.
var typedIDPrefixes = []string{"req", "corr", "ten", "ses", "inv", "int", "cns", "clr", "evr", "evs", "rec", "dec", "cfr", "cfe", "pln", "grt", "rcp", "ver", "wat", "rrq", "exp", "evt", "recev", "mem", "usr"}

// TypedIDPrefixes returns a copy of the prefixes the guard treats as typed IDs.
func TypedIDPrefixes() []string { return append([]string(nil), typedIDPrefixes...) }

// typedIDs are the typed values of the contracts, whole-string: the typed IDs of contracts/schemas/common.v1.json (a prefix and 2 to 32 letters or digits), the intent
// reference (`int_...@version`), the content hash (`sha256:` and 64 hex), the keyed hash of the input (`hmac-sha256:` and 64 hex), the idempotency key as PERD 27.2 writes
// it (`idk_v1_` and 64 hex, which common.v1's IdempotencyKeyId says since noema#564) and the grant nonce of decision-grant.v1
// (`n_` and 4 to 64 hex), and the lower-case UUID that canonical-input.v2 types the client's request ID as (the input gateway records it in INPUT_RECEIVED; common.v1's RequestId
// is `req_` and up to 32 characters, which that UUID does not fit: see the PR). guard_test.go reads common.v1.json and fails when the typed ID prefixes here and the contract disagree.
//
// Every exemption below is structural: the string must have the shape AND its body (the part after the prefix) must hold a letter, so a bare run of digits in a typed shape
// (`ten_4111111111111111`, `n_4111111111111111`, a 64-digit "hash") is refused. A genuine ID or hash has no letter in its body only with a probability that is negligible
// (a 26-character ULID: about 2 in 10^13; 64 hex digits: about 1 in 10^13). What no shape check can stop is a number written WITH a letter inside the shape
// (`ten_4111111111111111a`): that is the Architect's call, named in the PR (a bound on the digit run inside an exempt ID would be a threshold).
var typedIDs = []*regexp.Regexp{
	regexp.MustCompile(`\A(?:` + strings.Join(typedIDPrefixes, "|") + `)_([0-9A-Za-z]{2,32})\z`),
	regexp.MustCompile(`\Aint_([0-9A-Za-z]{2,32})@[0-9]+\z`),
	regexp.MustCompile(`\Asha256:([0-9a-f]{64})\z`),
	regexp.MustCompile(`\Ahmac-sha256:([0-9a-f]{64})\z`),
	regexp.MustCompile(`\Aidk_v1_([0-9a-f]{64})\z`),
	regexp.MustCompile(`\An_([0-9a-f]{4,64})\z`),
	// Every ID the core mints, whatever its prefix (PERD 32.3): a lower-case prefix, an underscore and a 26-character ULID, whose first character is 0 to 7. The contract's typed IDs
	// above are a subset; the outbox ID (`obx_`, the verification ID of an outcome.verify delivery) is one that is not, and a ULID holds five digits in a row about one time in five.
	regexp.MustCompile(`\A[a-z]{2,10}_([0-7][0-9A-HJKMNP-TV-Z]{25})\z`),
}

// bodyHasLetter reports whether the first capture group of re matches s and holds an ASCII letter.
func bodyHasLetter(re *regexp.Regexp, s string) bool {
	m := re.FindStringSubmatch(s)
	return m != nil && hasLetter(m[1])
}

func hasLetter(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i] | 0x20; c >= 'a' && c <= 'z' {
			return true
		}
	}
	return false
}

// clientRequestID is the lower-case UUID that canonical-input.v2 types the client's request ID as. It is exempt from the digit rule (a random UUID holds long digit runs by chance)
// but it is a value the client chose, so it is not a core-minted ID: the CI scan does not treat a payload that cites it as the object's own ID.
var clientRequestID = regexp.MustCompile(`\A([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\z`)

// moneyShape is the whole-string shape of an amount the core renders from integer minor units (A279, A270; the shared vectors of A259, contracts/fixtures/digit_run_vectors.json): three
// capital letters, one space, an integer part of one to three digits and then groups of three digits each with a comma before them, and exactly two decimals or none. ASCII digits and letters
// only; `\z` is the end of the string, not a place before a final newline. The same pattern is in the model service's redactor and in the UI scan; the vector file is what keeps the three together.
var moneyShape = regexp.MustCompile(`\A[A-Z]{3} [0-9]{1,3}(?:,[0-9]{3})*(?:\.[0-9]{2})?\z`)

// maxMoneyIntegerDigits is the most digits the integer part of a money string may hold (A279, A270). A longer amount is refused, like any other long run.
const maxMoneyIntegerDigits = 9

// isMoney reports whether the whole of s is a money string: moneyShape, and at most maxMoneyIntegerDigits digits before the decimal point. A comma ends a digit run, so a grouped amount with
// no decimals holds no run over four anyway; the exemption matters for the dot, which joins the last group to its decimals ("12,345.00" holds the run "345.00" of five).
func isMoney(s string) bool {
	if !moneyShape.MatchString(s) {
		return false
	}
	integer, _, _ := strings.Cut(s[len("NGN "):], ".")
	return countDigits(integer) <= maxMoneyIntegerDigits
}

// signatureValues are the shapes of the value of a `sig` object (common.v1 Sig): the hex of an HMAC-SHA-256 (the POC's signature) or a base64 form of 32 bytes or more.
// A signature is cryptographic output, not text, and its digits are random. It is exempt only at a path that ends in /sig/value (A184 names no such exemption: see the PR).
var signatureValues = []*regexp.Regexp{
	regexp.MustCompile(`\A([0-9a-f]{64})\z`),
	regexp.MustCompile(`\A([A-Za-z0-9+/_-]{43,}={0,2})\z`),
}

// A Violation is one string the guard refused: its JSON path (never its value) and the length of its longest digit run.
type Violation struct {
	Path string
	Run  int
}

// Error lists the violations by path. Its message carries no value.
type Error struct{ Violations []Violation }

func (e *Error) Error() string {
	parts := make([]string, len(e.Violations))
	for i, v := range e.Violations {
		parts[i] = fmt.Sprintf("%s (a run of %d digits)", v.Path, v.Run)
	}
	return ErrDigitRun.Error() + ": " + strings.Join(parts, "; ")
}

func (e *Error) Unwrap() error { return ErrDigitRun }

// Check refuses a payload (JSON) that holds a string with a digit run over Limit, unless the string as a whole is exempt. A payload that is not JSON is an error.
func Check(payload []byte) error {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("payloadguard: the payload is not JSON: %w", err)
	}
	var out []Violation
	walk("", v, &out)
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return &Error{Violations: out}
}

func walk(path string, v any, out *[]Violation) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			// An object key is text too: a number used as a key is held to the same rule. Its path never carries the key when the key is the violation.
			kp := path + "/" + safeKey(k)
			if longestRun(k) > Limit && !exempt(kp, k) {
				*out = append(*out, Violation{Path: path + "/{key}", Run: longestRun(k)})
			}
			walk(kp, e, out)
		}
	case []any:
		for i, e := range x {
			walk(fmt.Sprintf("%s/%d", path, i), e, out)
		}
	case string:
		if run := longestRun(x); run > Limit && !exempt(path, x) {
			*out = append(*out, Violation{Path: pathOrRoot(path), Run: run})
		}
	}
	// json.Number, bool and nil are not strings: a number-typed field is a number (an amount, a count), not text, and is not examined. This is A184's third exception, stated
	// here on purpose: a card number written as a JSON number passes. Whether a payload field may be typed as a number is the contract's call, not this guard's.
}

// safeKey is an object key as it may appear in a path: itself, or "{key}" when it holds a run over the limit that is not an exempt shape, so an error never quotes a number.
func safeKey(k string) string {
	if longestRun(k) > Limit && !exempt("", k) {
		return "{key}"
	}
	return k
}

func pathOrRoot(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

// longestRun is the number of digits in the longest run of digits (in any script) the separators join.
func longestRun(s string) int {
	longest := 0
	for _, run := range digitRun.FindAllString(s, -1) {
		n := 0
		for _, r := range run {
			if unicode.IsDigit(r) {
				n++
			}
		}
		if n > longest {
			longest = n
		}
	}
	return longest
}

// targetReference is the shape of a reference the target system minted, accepted by ruling A193 at two paths for the POC only: the acknowledgement of a receipt
// (`external_ack`, execution-receipt.v1: any non-empty string) and the target of a grant (`target_ref`). One token of at most 64 characters with no space. A bare number
// passes it: the Architect accepted that knowingly (DEBT-30: before the pilot these get an adapter-declared shape, or a masked reference plus its hash), because the case
// system's reference form is not known until G0-T and a stricter rule could refuse a real receipt, which is an unknown outcome after the target acted (A201). They appear on
// audit and ops surfaces only, never on the customer overlay or in an arm.
var targetReference = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9._:-]{0,63}\z`)

// exempt reports whether the whole string is a typed value of the contract (an ID, a hash, a nonce), an RFC 3339 timestamp, a money string (A279, A270), the transport class of a receipt at its two exact paths (A231 as narrowed), the value of a
// signature at /sig/value or the target's acknowledgement reference at /external_ack.
func exempt(path, s string) bool {
	if (strings.HasSuffix(path, "/external_ack") || strings.HasSuffix(path, "/target_ref")) && targetReference.MatchString(s) {
		return true
	}
	if strings.HasSuffix(path, "/key_id") && keyID.MatchString(s) {
		return true
	}
	if strings.HasSuffix(path, "/sig/value") {
		for _, re := range signatureValues {
			if bodyHasLetter(re, s) {
				return true
			}
		}
	}
	return isReceiptTransportValue(path, s) || isTypedID(s) || bodyHasLetter(clientRequestID, s) || isRFC3339(s) || isMoney(s)
}

// isRFC3339 is true for a timestamp in RFC 3339 form (a date, a "T", a time and an offset or Z), with or without fractional seconds.
func isRFC3339(s string) bool {
	if len(s) < 20 || s[10] != 'T' {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, s)
	return err == nil
}

// ScanEvent checks one stored Record event the way the CI scan of every test Record does: its payload against the guard, and its object_ref against derivation from the
// payload. It returns one line per finding, naming the event type and the JSON path, never a value. An event with no stored payload has nothing to scan.
func ScanEvent(eventType, objectRef string, payload []byte) []string {
	var out []string
	if len(payload) > 0 {
		if err := Check(payload); err != nil {
			var e *Error
			if errors.As(err, &e) {
				for _, v := range e.Violations {
					out = append(out, fmt.Sprintf("%s: the payload at %s holds a run of %d digits", eventType, v.Path, v.Run))
				}
			} else {
				out = append(out, fmt.Sprintf("%s: %v", eventType, err))
			}
		}
		if path, derived := DerivedObjectRef(objectRef, payload); derived {
			out = append(out, fmt.Sprintf("%s: the object_ref is built from the payload value at %s (an object_ref is minted by the core, never derived from a payload)", eventType, path))
		}
	}
	return out
}

// minDerived is the shortest normalised string that counts as derived: a shorter coincidence (a short ID shared by two strings) is not evidence of derivation.
const minDerived = 12

// DerivedObjectRef reports whether objectRef was built from a string of the payload: its body (after the prefix), lower-cased with everything but letters and digits removed,
// equals or contains the same normal form of a payload string (or the other way round), at least minDerived characters long, and that payload string is not the object_ref itself
// (an ID the payload cites verbatim is the object's own ID, which is the normal case). This is the shape of `req_` plus a client's request ID without its dashes, which the
// input gateway wrote (noema#466): a value the client chose, in the object_ref, which the reconstruction serves as written. It returns the JSON path of the payload string.
func DerivedObjectRef(objectRef string, payload []byte) (string, bool) {
	_, body, ok := strings.Cut(objectRef, "_")
	if !ok {
		return "", false
	}
	nb := normalForm(body)
	if len(nb) < minDerived {
		return "", false
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return "", false
	}
	var path string
	var found bool
	var visit func(p string, v any)
	visit = func(p string, v any) {
		if found {
			return
		}
		switch x := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				visit(p+"/"+safeKey(k), x[k])
			}
		case []any:
			for i, e := range x {
				visit(fmt.Sprintf("%s/%d", p, i), e)
			}
		case string:
			if x == objectRef || isTypedID(x) {
				return // the object's own ID, or another typed ID the core minted and the payload cites
			}
			ns := normalForm(x)
			if len(ns) >= minDerived && (strings.Contains(nb, ns) || strings.Contains(ns, nb)) {
				path, found = pathOrRoot(p), true
			}
		}
	}
	visit("", v)
	return path, found
}

func isTypedID(s string) bool {
	for _, re := range typedIDs {
		if bodyHasLetter(re, s) {
			return true
		}
	}
	return false
}

func normalForm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TransportClasses returns a copy of the transport classes the guard exempts at the receipt's two paths; guard_test.go reads the controller's constants and fails when they differ.
func TransportClasses() []string {
	out := make([]string, 0, len(transportClasses))
	for c := range transportClasses {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
