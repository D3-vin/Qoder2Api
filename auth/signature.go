package auth

import (
	"crypto/md5"
	"encoding/hex"
	"time"
)

const (
	APPCODE = "cosy"
	SECRET  = "d2FyLCB3YXIgbmV2ZXIgY2hhbmdlcw==" // base64("war, war never changes")
	SEP     = "&"
)

func CurrentDate() string {
	// Format: EEE, dd MMM yyyy HH:mm:ss GMT (like Java's DateTimeFormatter)
	return time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
}

func Sign(date string) string {
	h := md5.New()
	h.Write([]byte(APPCODE + SEP + SECRET + SEP + date))
	return hex.EncodeToString(h.Sum(nil))
}
