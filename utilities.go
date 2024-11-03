package main

import (
	"crypto/md5"
	"encoding/hex"
)

func authKeyVerification(authKey string, userName string) bool {
	checkKey := md5.Sum([]byte(userName + EXTRA_KEY_STRING))
	return hex.EncodeToString(checkKey[:]) == authKey
}
