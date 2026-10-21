// Package ids makes the identifiers and references mock-case hands out. They are a pure function of the
// run seed, the kind of object, a gapless counter and the clock reading, so a scenario replayed from the same
// seed and a simulated clock produces the same identifiers (REQ-ENV-004).
package ids

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ULID builds a ULID: 48 bits of millisecond time, then 80 bits taken from SHA-256(seed, kind, counter).
func ULID(seed int64, kind string, counter int64, at time.Time) string {
	var raw [16]byte
	ms := uint64(at.UnixMilli())
	for i := 0; i < 6; i++ {
		raw[i] = byte(ms >> (8 * (5 - i)))
	}
	h := sha256.New()
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], uint64(seed))
	binary.BigEndian.PutUint64(buf[8:], uint64(counter))
	h.Write([]byte("mock-case/id/v1\x00" + kind + "\x00"))
	h.Write(buf[:])
	copy(raw[6:], h.Sum(nil)[:10])
	return encode(raw)
}

// encode writes 128 bits as 26 Crockford base32 characters (the first carries only 3 bits).
func encode(raw [16]byte) string {
	out := make([]byte, 26)
	var hi, lo uint64
	hi = binary.BigEndian.Uint64(raw[:8])
	lo = binary.BigEndian.Uint64(raw[8:])
	for i := 25; i >= 0; i-- {
		out[i] = crockford[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out)
}

// CaseID is case_<ULID>.
func CaseID(seed, counter int64, at time.Time) string {
	return "case_" + ULID(seed, "case", counter, at)
}

// AttachmentID is att_<ULID>.
func AttachmentID(seed, counter int64, at time.Time) string {
	return "att_" + ULID(seed, "evidence", counter, at)
}

// CaseReference is the human reference of a case, CMP-<year>-<5 digits> (PERD §39.5 example form). SYNTHETIC.
func CaseReference(at time.Time, counter int64) string {
	return fmt.Sprintf("CMP-%04d-%05d", at.UTC().Year(), counter)
}

// ComplaintRef is the reference of a complaint, CPR-<year>-<5 digits>. SYNTHETIC.
func ComplaintRef(at time.Time, counter int64) string {
	return fmt.Sprintf("CPR-%04d-%05d", at.UTC().Year(), counter)
}
