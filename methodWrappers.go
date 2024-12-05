package main

import (
	"net/http"
)

func openPostWrapper(w http.ResponseWriter, r *http.Request, handle func(http.ResponseWriter, *http.Request)) {
	if r.Method == http.MethodPost {
		handle(w, r)
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (Env env) securedPostWrapper(w http.ResponseWriter, r *http.Request, handle func(http.ResponseWriter, *http.Request)) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if !authKeyVerification(r.Header.Get("AuthKey"), r.Header.Get("NationName"), Env.KeyString) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	handle(w, r)
}

func (Env env) securedGetWrapper(w http.ResponseWriter, r *http.Request, handle func(http.ResponseWriter, *http.Request)) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if !authKeyVerification(r.Header.Get("AuthKey"), r.Header.Get("NationName"), Env.KeyString) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	handle(w, r)
}
