package inputgateway

// UND-02, core half, review of noema#466 by the Security Reviewer at ad9c3ffc (REQ-INP-001, REQ-VOI-004, REQ-SEC-003; INV-12; AGENT-13): the voice member is
// parsed and a non-null audio_ref refused (CR-2026-11-08-VOI-canonical-input-audio-ref, A1: no retention path exists); the body handed on has no audio_ref; the
// hash the Record keeps is an HMAC under a per-tenant key, never a plain hash of the text. No database. All data is SYNTHETIC.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

const synthReq = "0b8f3c1e-52a4-4d7e-9a61-3c0f5d2e7b19"

func voiceBody(voice string) []byte {
	return []byte(`{"request_id":"` + synthReq + `","channel":"voice","text":"where is my money","voice":` + voice + `}`)
}

func TestAVoiceWithoutARetainedAudioReferenceIsAccepted(t *testing.T) {
	for name, voice := range map[string]string{
		"a null audio_ref":   `{"provider_id":"stt_a","is_final":true,"audio_ref":null}`,
		"no audio_ref":       `{"provider_id":"stt_a","is_final":true}`,
		"an empty voice":     `{}`,
		"a nested confident": `{"utterance_confidence":0.5,"token_confidences":[0.1,0.9],"audio_ref":null}`,
	} {
		in, err := parseBody(voiceBody(voice), 1<<16)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if bytes.Contains(in.canon, []byte("audio_ref")) {
			t.Errorf("%s: the body handed on still has audio_ref: %s", name, in.canon)
		}
	}
}

// CR-2026-11-08, A1: until a retention path exists the edge refuses any non-null audio_ref, whatever it looks like and whatever the Record holds; so a reference that
// exists only in another tenant's Record is refused with the same answer as one that exists nowhere (the refusal reads nothing).
func TestAVoiceWithANonNullAudioReferenceIsRefusedAlike(t *testing.T) {
	var first string
	for name, ref := range map[string]string{
		"a raw_ reference":     `"raw_01J9X3"`,
		"another tenant's one": `"raw_01J9X4ZZZ"`,
		"an invented rec_ one": `"rec_01J9X3"`,
		"an empty string":      `""`,
		"base64":               `"UklGRiQAAABXQVZF"`,
		"a number":             `5`,
		"an object":            `{"id":"raw_01J9X3"}`,
		"an array":             `["raw_01J9X3"]`,
		"a boolean":            `false`,
		"a null in other case": `"null"`,
	} {
		_, err := parseBody(voiceBody(`{"is_final":true,"audio_ref":`+ref+`}`), 1<<16)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: %v, want ErrInvalidInput", name, err)
			continue
		}
		if strings.Contains(err.Error(), "raw_") || strings.Contains(err.Error(), "UklG") {
			t.Errorf("%s: the error quotes the reference: %v", name, err)
		}
		if first == "" {
			first = err.Error()
		}
		if err.Error() != first {
			t.Errorf("%s: the answer %q differs from %q: a reference's existence would show", name, err.Error(), first)
		}
	}
}

// A repeated member inside voice would let a later null hide an earlier reference from a decoder that keeps the last: it is refused.
func TestAVoiceThatRepeatsAMemberIsRefused(t *testing.T) {
	for _, voice := range []string{`{"audio_ref":"raw_01J9X3","audio_ref":null}`, `{"audio_ref":null,"audio_ref":"raw_01J9X3"}`, `{"is_final":true,"is_final":false}`} {
		if _, err := parseBody(voiceBody(voice), 1<<16); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("voice %s: %v, want ErrInvalidInput", voice, err)
		}
	}
}

func TestAVoiceThatIsNotAnObjectIsRefused(t *testing.T) {
	for _, voice := range []string{`"x"`, `[]`, `5`, `true`} {
		if _, err := parseBody(voiceBody(voice), 1<<16); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("voice %s: %v", voice, err)
		}
	}
}

// What is handed on and hashed is the body without audio_ref, so a null and an absent audio_ref are one body (null means "discarded", nothing else) and a retry
// that adds or drops the null is a redelivery, not a reuse.
func TestANullAndAnAbsentAudioReferenceAreTheSameBody(t *testing.T) {
	a, err := parseBody(voiceBody(`{"is_final":true,"audio_ref":null}`), 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseBody(voiceBody(`{"audio_ref":null,"is_final":true}`), 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	c, err := parseBody(voiceBody(`{"is_final":true}`), 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.canon, b.canon) || !bytes.Equal(a.canon, c.canon) {
		t.Errorf("canonical forms differ: %s | %s | %s", a.canon, b.canon, c.canon)
	}
	if !strings.Contains(string(a.canon), `"is_final":true`) {
		t.Errorf("the other voice members were dropped: %s", a.canon)
	}
}

var synthKey = bytes.Repeat([]byte{0x42}, 32) // SYNTHETIC

func TestTheHashTheRecordKeepsIsAnHMACUnderAPerTenantKey(t *testing.T) {
	canon := []byte(`{"channel":"text","request_id":"` + synthReq + `","text":"4111 2222 3333 4444"}`)
	h := inputHash(synthKey, "ten_aa", canon)
	plain := sha256.Sum256(canon)
	if h == "sha256:"+hex.EncodeToString(plain[:]) || strings.Contains(h, hex.EncodeToString(plain[:])) {
		t.Error("the hash is the plain sha-256 of the body: a short text can be recovered by guessing")
	}
	if !strings.HasPrefix(h, "hmac-sha256:") || len(h) != len("hmac-sha256:")+64 {
		t.Errorf("hash %q is not hmac-sha256: and 64 hex digits", h)
	}
	if h != inputHash(synthKey, "ten_aa", canon) {
		t.Error("the hash is not deterministic")
	}
	if h == inputHash(synthKey, "ten_bb", canon) {
		t.Error("two tenants' hashes of the same body are equal: one tenant could be told from another")
	}
	if h == inputHash(bytes.Repeat([]byte{0x43}, 32), "ten_aa", canon) {
		t.Error("two keys give the same hash")
	}
	if h == inputHash(synthKey, "ten_aa", append(canon[:len(canon)-1:len(canon)-1], ' ', '}')) {
		t.Error("another body gives the same hash")
	}
}
