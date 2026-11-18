package ledger

import "errors"

// EnvSerializationAttempts names the environment setting that holds Ledger.SerializationAttempts.
const EnvSerializationAttempts = "NOEMA_DB_SERIALIZATION_ATTEMPTS"

// ParseSerializationAttempts reads the value of EnvSerializationAttempts.
func ParseSerializationAttempts(raw string) (int, error) {
	return 0, errors.New(errPrefix + "not implemented")
}
