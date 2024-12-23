package main

import (
	"net/http"
)

func (Env env) securedWrapper(w http.ResponseWriter, r *http.Request, handle func(http.ResponseWriter, *http.Request)) {
	if !authKeyVerification(r.Header.Get("AuthKey"), r.Header.Get("NationName"), Env.KeyString) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	handle(w, r)
}
