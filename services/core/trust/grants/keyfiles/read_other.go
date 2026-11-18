//go:build !unix

package keyfiles

// readKeyFile refuses on a system where the owner and mode of a file cannot be checked: nothing is loaded there, so no weaker check ever stands in (the core runs on Linux in Compose).
func readKeyFile(string) ([]byte, error) { return nil, ErrUnsupported }
