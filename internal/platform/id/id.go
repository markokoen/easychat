package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

var randRead = rand.Read
var nowUnixNano = func() int64 { return time.Now().UTC().UnixNano() }

func New() string {
	buf := make([]byte, 12)
	if _, err := randRead(buf); err != nil {
		return fmt.Sprintf("%d", nowUnixNano())
	}
	return hex.EncodeToString(buf)
}
