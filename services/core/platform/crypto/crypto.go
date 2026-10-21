package crypto

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode/utf16"
)

// maxSafeInt is 2^53-1, the integer ceiling of every Noema contract.
const maxSafeInt = 9007199254740991

// CanonicalJSON returns the RFC 8785 form of v for the value types Noema contracts use: objects, arrays,
// strings, integers within 2^53-1, booleans and null. A fractional or exponent number is refused, so a float
// can never reach a hash or a signature.
func CanonicalJSON(v any) ([]byte, error) {
	if err := refuseFloats(reflect.ValueOf(v)); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := writeCanonical(&out, generic); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeCanonical(out *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if t {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		writeString(out, t)
	case json.Number:
		s := t.String()
		if strings.ContainsAny(s, ".eE") {
			return fmt.Errorf("number %s is not an integer", s)
		}
		n, err := t.Int64()
		if err != nil || n > maxSafeInt || n < -maxSafeInt {
			return fmt.Errorf("number %s is outside 2^53-1", s)
		}
		out.WriteString(s)
	case []any:
		out.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonical(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		out.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			writeString(out, k)
			out.WriteByte(':')
			if err := writeCanonical(out, t[k]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported value %T", v)
	}
	return nil
}

// utf16Less orders keys by UTF-16 code units, as RFC 8785 requires.
func utf16Less(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

func writeString(out *bytes.Buffer, s string) {
	out.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			out.WriteString(`\"`)
		case r == '\\':
			out.WriteString(`\\`)
		case r == '\b':
			out.WriteString(`\b`)
		case r == '\f':
			out.WriteString(`\f`)
		case r == '\n':
			out.WriteString(`\n`)
		case r == '\r':
			out.WriteString(`\r`)
		case r == '\t':
			out.WriteString(`\t`)
		case r < 0x20:
			fmt.Fprintf(out, `\u%04x`, r)
		default:
			out.WriteRune(r)
		}
	}
	out.WriteByte('"')
}

// Hash is "sha256:" and the lower-case hex SHA-256 of b.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// HashOf is the hash of the canonical form of v.
func HashOf(v any) (string, error) {
	c, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	return Hash(c), nil
}

// IdempotencyKeyFields are the seven inputs of PERD section 27.2. The attempt number is not one of them.
type IdempotencyKeyFields struct {
	TenantID, PrincipalID, TransactionID, LogicalAction, ReasonCode, PolicyScope string
	LogicalResolutionEpoch                                                       string
}

// IdempotencyKey is idk_v1_ plus the hex SHA-256 of the seven length-prefixed fields (4-byte big-endian
// length, then the UTF-8 bytes), in the order of PERD section 27.2.
func IdempotencyKey(f IdempotencyKeyFields) string {
	var buf bytes.Buffer
	for _, p := range []string{f.TenantID, f.PrincipalID, f.TransactionID, f.LogicalAction, f.ReasonCode, f.PolicyScope, f.LogicalResolutionEpoch} {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(p)))
		buf.Write(n[:])
		buf.WriteString(p)
	}
	sum := sha256.Sum256(buf.Bytes())
	return "idk_v1_" + hex.EncodeToString(sum[:])
}

// Sign is the lower-case hex HMAC-SHA-256 of msg.
func Sign(key, msg []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return hex.EncodeToString(m.Sum(nil))
}

// Verify compares in constant time. A signature that is not 64 lower-case hex characters is refused.
func Verify(key, msg []byte, sigHex string) bool {
	if len(sigHex) != 64 || sigHex != strings.ToLower(sigHex) {
		return false
	}
	want, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return hmac.Equal(m.Sum(nil), want)
}

// KeyRing holds the active signing keys by ID. Keys never leave it except through Sign and Verify.
type KeyRing struct{ keys map[string][]byte }

// ErrUnknownKey is returned for a key ID that is not active.
var ErrUnknownKey = errors.New("crypto: key is not active")

// NewKeyRing copies the keys it is given.
func NewKeyRing(keys map[string][]byte) *KeyRing {
	k := &KeyRing{keys: make(map[string][]byte, len(keys))}
	for id, v := range keys {
		k.keys[id] = append([]byte(nil), v...)
	}
	return k
}

// SignWith signs msg with the named active key.
func (k *KeyRing) SignWith(keyID string, msg []byte) (string, error) {
	key, ok := k.keys[keyID]
	if !ok {
		return "", ErrUnknownKey
	}
	return Sign(key, msg), nil
}

// VerifyWith verifies msg against the named active key; an unknown key is a failure, not a panic.
func (k *KeyRing) VerifyWith(keyID string, msg []byte, sigHex string) bool {
	key, ok := k.keys[keyID]
	return ok && Verify(key, msg, sigHex)
}

// refuseFloats walks v and fails on any float32 or float64 value, even an integral one: encoding/json would
// print 2000000.0 as 2000000, and a float must never be silently accepted as an integer (M4).
func refuseFloats(v reflect.Value) error {
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		return errors.New("crypto: a float value is not allowed in a canonical document")
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return refuseFloats(v.Elem())
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := refuseFloats(v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			if err := refuseFloats(v.MapIndex(k)); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				if err := refuseFloats(v.Field(i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
