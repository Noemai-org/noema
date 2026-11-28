package opslistener_test

import "os"

func osRead(p string) ([]byte, error) { return os.ReadFile(p) }
