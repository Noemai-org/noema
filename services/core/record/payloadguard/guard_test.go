package payloadguard_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/record/payloadguard"
)

// A184, A154, REQ-AUD-003, REQ-SEC-003: the guard's rule, as a pure function of the payload JSON.

func check(t *testing.T, js string) error {
	t.Helper()
	return payloadguard.Check([]byte(js))
}

func TestARunOfFiveDigitsInAStringIsRefusedAndARunOfFourIsNot(t *testing.T) {
	for js, wantErr := range map[string]bool{
		`{"a":"1234"}`:                                   false,
		`{"a":"12345"}`:                                  true,
		`{"a":"year 2026"}`:                              false,
		`{"a":"a1234b5678c"}`:                            false, // two runs of four, separated by letters
		`{"a":"0123456789"}`:                             true,
		`{"a":"4111 1111 1111 1111"}`:                    true, // spaces join
		`{"a":"4111-1111"}`:                              true,
		`{"a":"4111.1111"}`:                              true,
		`{"a":"4111/1111"}`:                              true,
		`{"a":"4111_1111"}`:                              false, // an underscore ends a run: a code is not a number
		`{"a":"409:412:409"}`:                            false,
		`{"a":"20,000,000"}`:                             false, // a comma ends a run
		`{"a":"٤١١١١"}`:                                  true,  // Arabic-Indic digits
		`{"a":"４１１１１"}`:                                  true,  // full-width digits
		`{"a":"4111​1111"}`:                              true,  // a zero-width space joins
		`{"a":"4111­1111"}`:                              true,  // a soft hyphen joins
		`{"a":"4111–1111"}`:                              true,  // an en dash joins
		`{"a":["x",{"b":"12345"}]}`:                      true,
		`{"a":12345,"b":123456789012,"c":true,"d":null}`: false, // numbers, booleans and null are not strings
		`[]`: false, `{}`: false, `"abcd"`: false, `"12345"`: true, // a payload may be any JSON value
	} {
		err := check(t, js)
		if (err != nil) != wantErr {
			t.Errorf("%s: err %v, want an error: %v", js, err, wantErr)
		}
		if err != nil && !errors.Is(err, payloadguard.ErrDigitRun) {
			t.Errorf("%s: the error does not wrap ErrDigitRun: %v", js, err)
		}
	}
}

func TestTheExemptionsAreWholeStringsOnly(t *testing.T) {
	hex64 := strings.Repeat("0123456789abcdef", 4)
	ok := []string{
		`"grt_01234567890123456789AB2345"`, `"corr_synth0001"`, `"int_0123456789ab@12"`, `"recev_0123456789ABCDEF"`,
		`"sha256:` + hex64 + `"`, `"hmac-sha256:` + hex64 + `"`, `"idk_v1_` + hex64 + `"`, `"n_0123456789abcdef0123456789abcdef"`,
		`"2026-11-13T10:12:00Z"`, `"2026-11-13T10:12:00.123456789Z"`, `"2026-11-13T10:12:00+01:00"`,
		`"12345678-1234-1234-1234-12345678901a"`, `"3f2a9c1e-7b4d-4e8a-9c3b-1d2e3f4a5b6c"`, // the client's request ID, a lower-case UUID
	}
	for _, v := range ok {
		if err := check(t, `{"v":`+v+`}`); err != nil {
			t.Errorf("%s was refused: %v", v, err)
		}
	}
	bad := []string{
		`"grt_4111111111111111!"`,                           // not the shape of the ID
		`"my grt_01234567890123456789AB2345"`,               // text before the ID
		`"grt_01234567890123456789AB2345 4111111111111111"`, // text after it
		`"sha256:` + hex64 + `0"`,                           // one character too long for a hash
		`"2026-11-13"`,                                      // a date is not an RFC 3339 timestamp
		`"2026-11-13T10:12:00Z 4111111111111111"`,           // a timestamp and more
		`"n_xyz0123456789"`,                                 // not hex
		`"idk_v1_` + hex64[:60] + `"`,                       // too short for the key
	}
	for _, v := range bad {
		if err := check(t, `{"v":`+v+`}`); err == nil {
			t.Errorf("%s was accepted", v)
		}
	}
}

func TestTheSignatureTheTargetReferenceAndTheKeyIDAreExemptAtTheirPathsOnly(t *testing.T) {
	hex64 := strings.Repeat("0123456789abcdef", 4)
	for name, js := range map[string]string{
		"a signature value":         `{"verdict":{"sig":{"key_id":"core-outcome.ed25519.2","value":"` + hex64 + `"}}}`,
		"a base64 signature":        `{"sig":{"key_id":"k","value":"` + strings.Repeat("0123456789", 5) + `a"}}`,
		"the target's reference":    `{"external_ack":"CMP-2026-00001"}`,
		"a nested target reference": `{"receipt":{"external_ack":"CMP-2026-00001"}}`,
		"the target of a grant":     `{"target_ref":"case_01KE8M3Q4Z5J6N7P8R9STV2WX3","capability_id":"test.cap.create_case"}`,
		"a target with a long run":  `{"target_ref":"case_0123456789ABCDEFGHJKMNPQRS"}`,
	} {
		if err := check(t, js); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
	for name, js := range map[string]string{
		"a signature shape at another path":    `{"note":"` + hex64 + `"}`,
		"a signature value that is not hex":    `{"sig":{"value":"4111111111111111 note"}}`,
		"digits alone as a signature value":    `{"sig":{"value":"4111111111111111"}}`,
		"a reference with a space":             `{"external_ack":"CMP 2026 00001 4111111111111111"}`,
		"a reference longer than 64":           `{"external_ack":"` + strings.Repeat("a", 60) + `12345"}`,
		"the reference shape at another path":  `{"note":"CMP-2026-00001"}`,
		"a target with a space":                `{"target_ref":"case 4111111111111111"}`,
		"a bare number as the target":          `{"target_ref":"4111111111111111"}`, // the Security Reviewer's P1 on noema#536
		"a bare number as the acknowledgement": `{"external_ack":"4111111111111111"}`,
		"a short number as the target":         `{"target_ref":"123456789012"}`,
		"letters and digits as the target":     `{"target_ref":"acct4111111111111"}`,
		"a token that is not the minted form":  `{"external_ack":"att_20261113123456"}`,
		"the form with a long middle":          `{"external_ack":"CMP-2026411111111-00001"}`,
		"the target shape under another key":   `{"resolved":"CMP-2026-0000123456"}`,
		"an algorithm name hiding a long run":  `{"sig":{"key_id":"ed25519 4111111111111111"}}`,
	} {
		if err := check(t, js); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// The algorithm name is removed before the digits are counted: it does not make a long run of its own, and it does not hide the digits beside it.
	if err := check(t, `{"key_id":"core-execution.ed25519.1"}`); err != nil {
		t.Errorf("a key ID with ed25519 was refused: %v", err)
	}
	if err := check(t, `{"key_id":"4111ed25519111111111"}`); err == nil {
		t.Error("digits beside an algorithm name were accepted")
	}
}

func TestAViolationNamesThePathAndNeverTheValue(t *testing.T) {
	err := check(t, `{"outer":{"inner":["ok","card 4111111111111111"]},"other":"fine"}`)
	var e *payloadguard.Error
	if !errors.As(err, &e) || len(e.Violations) != 1 || e.Violations[0].Path != "/outer/inner/1" || e.Violations[0].Run != 16 {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "4111") || strings.Contains(err.Error(), "card") {
		t.Fatalf("the error quotes the value: %q", err.Error())
	}
}

func TestEveryViolationIsListedInPathOrder(t *testing.T) {
	err := check(t, `{"b":"12345","a":"67890","c":"ok"}`)
	var e *payloadguard.Error
	if !errors.As(err, &e) {
		t.Fatalf("got %v", err)
	}
	var paths []string
	for _, v := range e.Violations {
		paths = append(paths, v.Path)
	}
	if !sort.StringsAreSorted(paths) || len(paths) != 2 {
		t.Fatalf("paths %v", paths)
	}
}

func TestAPayloadThatIsNotJSONIsAnError(t *testing.T) {
	if err := check(t, `not json`); err == nil || errors.Is(err, payloadguard.ErrDigitRun) {
		t.Fatalf("got %v", err)
	}
}

// The typed IDs of the guard are the ID definitions of contracts/schemas/common.v1.json: a prefix and 2 to 32 letters or digits. A new ID in the contract that the guard does not
// list would make a legitimate write fail; a prefix the contract does not define would exempt text that is no ID.
func TestTheTypedIDPrefixesAreTheContractsIDDefinitions(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "schemas", "common.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Pattern string `json:"pattern"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^\^([a-z]{2,6})_\[0-9A-Za-z\]\{2,32\}\$$`)
	want := map[string]bool{}
	for name, d := range doc.Defs {
		if name == "Id" { // the generic ID of any prefix
			continue
		}
		if m := re.FindStringSubmatch(d.Pattern); m != nil {
			want[m[1]] = true
		}
	}
	got := map[string]bool{}
	for _, p := range payloadguard.TypedIDPrefixes() {
		got[p] = true
	}
	if len(want) < 20 {
		t.Fatalf("found only %d ID definitions in common.v1.json: the test is looking in the wrong place", len(want))
	}
	for p := range want {
		if !got[p] {
			t.Errorf("common.v1.json defines the ID prefix %q and the guard does not list it", p)
		}
	}
	for p := range got {
		if !want[p] {
			t.Errorf("the guard lists the ID prefix %q and common.v1.json does not define it", p)
		}
	}
}

// ScanEvent is what the CI scan of every test Record runs on a stored event: the guard on the payload, and the object_ref against derivation from the payload.
func TestScanEventFindsADigitRunAndADerivedObjectRef(t *testing.T) {
	uuid := "12345678-1234-1234-1234-12345678901a"
	hex := strings.ReplaceAll(uuid, "-", "")
	for name, tc := range map[string]struct {
		objectRef, payload string
		wantFindings       int
	}{
		"clean":                            {"grt_synth0001", `{"grant_id":"grt_synth0001","n":3}`, 0},
		"a digit run":                      {"grt_synth0001", `{"note":"4111111111111111"}`, 1},
		"the gateway's shape":              {"req_" + hex, `{"request_id":"` + uuid + `"}`, 1},
		"the same, a different UUID":       {"req_0123456789abcdef0123456789abcdef", `{"request_id":"01234567-89ab-cdef-0123-456789abcdef"}`, 1},
		"the object's own ID cited":        {"int_01234567890123456789AB2345", `{"intent_id":"int_01234567890123456789AB2345"}`, 0},
		"a versioned citation of the ID":   {"int_01234567890123456789AB2345", `{"ref":"int_01234567890123456789AB2345@2"}`, 0},
		"another minted ID in the payload": {"gat_0123456789abcdef", `{"grant_id":"grt_0123456789abcdef"}`, 0},
		"a short coincidence":              {"req_abc123", `{"note":"abc123"}`, 0},
		"no payload":                       {"req_" + hex, ``, 0},
	} {
		got := payloadguard.ScanEvent("TEST_EVENT", tc.objectRef, []byte(tc.payload))
		if len(got) != tc.wantFindings {
			t.Errorf("%s: %d findings %v, want %d", name, len(got), got, tc.wantFindings)
		}
		for _, f := range got {
			if strings.Contains(f, "4111") || strings.Contains(f, uuid) || strings.Contains(f, hex) {
				t.Errorf("%s: a finding quotes a value: %q", name, f)
			}
		}
	}
}

// PERD 32.3, AGENTS section 7 (Naming): every ID the core mints is `<prefix>_<ULID>`, and a ULID holds a run of five or more digits by chance in about one ID in five. An ID the
// core minted for the outbox (`obx_`, the verification ID of the outcome.verify delivery) is not one of the contract's typed IDs and still must not stop a Record append: a write
// refused one run in five would be a flake in the field. The shape is a lower-case prefix, an underscore and 26 Crockford characters whose first is 0 to 7 (48 bits of time).
func TestACoreMintedIDOfAnyPrefixIsExemptAndNothingLookingLikeOneIs(t *testing.T) {
	for name, js := range map[string]string{
		"an outbox ID with a run of five": `{"verification_id":"obx_01M6GA61234XG8FMN6SCM59MJM"}`,
		"a made-up prefix":                `{"a":"zzz_0123456789ABCDEFGHJKMNPQRS"}`,
		"a verification ID of the outbox": `{"verification_id":"obx_7ZZZZZZZZZZZZZZZZZZZZZZZZZ"}`,
	} {
		if err := check(t, js); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
	for name, js := range map[string]string{
		"a prefix over a prefix (a test's old shape)": `{"v":"vrf_rcp_01M6GA61234XG8FMN6SCM59MJM"}`,
		"a ULID one character short":                  `{"v":"obx_01M6GA61234XG8FMN6SCM59MJ"}`,
		"a ULID one character long":                   `{"v":"obx_01M6GA61234XG8FMN6SCM59MJMX"}`,
		"a first character over 7":                    `{"v":"obx_81M6GA61234XG8FMN6SCM59MJM"}`,
		"an upper-case prefix":                        `{"v":"OBX_01M6GA61234XG8FMN6SCM59MJM"}`,
		"a letter I in the body":                      `{"v":"obx_01M6GA61234XG8FMN6SCM59MJI"}`,
		"the shape under no prefix":                   `{"v":"_01M6GA61234XG8FMN6SCM59MJM"}`,
		"a space":                                     `{"v":"obx_01M6GA61234XG8FMN6 CM59MJM"}`,
	} {
		if err := check(t, js); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// The Security Reviewer's second blocking finding on noema#536: every exemption is structural. A bare run of digits in a typed shape, a hash shape, a UUID or a signature is not
// an ID, a hash or a signature, so it is refused; the same shapes with a letter in the body are accepted (a genuine one has a letter, bar a probability of about 10^-13).
func TestNoExemptionAcceptsABareRunOfDigits(t *testing.T) {
	digits64 := strings.Repeat("4", 64)
	for name, js := range map[string]string{
		"a typed ID of digits":             `{"v":"ten_4111111111111111"}`,
		"a typed ID, the longest body":     `{"v":"grt_` + strings.Repeat("4", 32) + `"}`,
		"the nonce of digits":              `{"v":"n_4111111111111111"}`,
		"a content hash of digits":         `{"v":"sha256:` + digits64 + `"}`,
		"a keyed hash of digits":           `{"v":"hmac-sha256:` + digits64 + `"}`,
		"an idempotency key of digits":     `{"v":"idk_v1_` + digits64 + `"}`,
		"an intent reference of digits":    `{"v":"int_4111111111111111@2"}`,
		"a ULID of digits, any prefix":     `{"v":"zzz_01234567890123456789012345"}`,
		"a UUID of digits":                 `{"v":"41111111-1111-1111-1111-111111111111"}`,
		"a signature of digits (hex)":      `{"sig":{"value":"` + digits64 + `"}}`,
		"a signature of digits (base64)":   `{"sig":{"value":"` + strings.Repeat("4", 50) + `"}}`,
		"a bare number as the target":      `{"target_ref":"4111111111111111"}`,
		"a bare number as acknowledgement": `{"external_ack":"4111111111111111"}`,
	} {
		if err := check(t, js); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Note 4 of the Security Reviewer: an algorithm name is exempt only as a key ID, at /key_id and in the key ID's own shape. It is not removed from any string, so it cannot be
// put between groups of digits to split a number into runs of four or fewer.
func TestAnAlgorithmNameIsExemptOnlyAsAKeyID(t *testing.T) {
	for name, js := range map[string]string{
		"a key ID":              `{"sig":{"key_id":"core-execution.ed25519.1","value":"` + strings.Repeat("ab", 32) + `"}}`,
		"a key ID of the other": `{"k":{"key_id":"core-trust.x25519.12"}}`,
	} {
		if err := check(t, js); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
	for name, js := range map[string]string{
		"a number split by the algorithm name": `{"v":"4111ed25519111ed25519111ed25519111"}`,
		"the same, upper case":                 `{"v":"4111ED25519111X25519111"}`,
		"the algorithm name alone":             `{"v":"ed25519"}`,
		"a key ID shape at another key":        `{"v":"core-execution.ed25519.1"}`,
		"a key ID with text after it":          `{"key_id":"core-execution.ed25519.1 4111111111111111"}`,
	} {
		if err := check(t, js); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Note 4: combining marks and format characters between digits do not split a number.
func TestCombiningMarksAndFormatCharactersDoNotSplitANumber(t *testing.T) {
	for name, v := range map[string]string{
		"a combining acute":     "4\u0301 1\u0301 1\u0301 1\u0301 1",
		"a combining mark only": "4\u03011\u03011\u03011\u03011",
		"a left-to-right mark":  "4\u200e1\u200e1\u200e1\u200e1",
		"a word joiner":         "4\u20601\u20601\u20601\u20601",
		"a variation selector":  "4\ufe0f1\ufe0f1\ufe0f1\ufe0f1",
	} {
		if err := check(t, `{"v":"`+v+`"}`); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Note 3: an object key is text too. A number used as a key is refused, and the refusal names no key: the path says "{key}".
func TestAnObjectKeyIsHeldToTheSameRuleAndNeverQuoted(t *testing.T) {
	err := check(t, `{"outer":{"4111111111111111":"x"}}`)
	if err == nil {
		t.Fatal("a card number as a key was accepted")
	}
	if strings.Contains(err.Error(), "4111") || !strings.Contains(err.Error(), "/outer/{key}") {
		t.Fatalf("the error quotes the key or does not name its place: %v", err)
	}
	err = check(t, `{"4111111111111111":{"note":"4111111111111111"}}`)
	if err == nil || strings.Contains(err.Error(), "4111") || !strings.Contains(err.Error(), "/{key}/note") {
		t.Fatalf("the path of a value under a numeric key quotes it or is wrong: %v", err)
	}
	if err := check(t, `{"grt_01234567890123456789AB2345":"x","evr_01":{"a":1}}`); err != nil {
		t.Fatalf("an ID used as a key was refused: %v", err)
	}
}

// Note 3, the other half, stated as a test: a number-typed field is not examined (A184's third exception).
func TestANumberTypedFieldIsNotExamined(t *testing.T) {
	if err := check(t, `{"amount":4111111111111111,"count":12345678}`); err != nil {
		t.Fatalf("a number-typed field was refused: %v", err)
	}
}
