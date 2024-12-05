package main

import (
	"crypto/md5"
	"encoding/hex"
)

func authKeyVerification(authKey string, userName string, extraString string) bool {
	checkKey := md5.Sum([]byte(userName + extraString))
	return hex.EncodeToString(checkKey[:]) == authKey
}
