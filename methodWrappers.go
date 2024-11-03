package main

import (
	"net/http"

	"cloud.google.com/go/firestore"
	"github.com/jackc/pgx/v5/pgxpool"
)

func openPostWrapper(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool, handle func(http.ResponseWriter, *http.Request, *pgxpool.Pool)) {
	if r.Method == http.MethodPost {
		handle(w, r, dbPool)
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func securedPostWrapper(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool, handle func(http.ResponseWriter, *http.Request, *pgxpool.Pool)) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if !authKeyVerification(r.Header.Get("AuthKey"), r.Header.Get("NationName")) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	handle(w, r, dbPool)
}

func securedGetWrapper(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool, handle func(http.ResponseWriter, *http.Request, *pgxpool.Pool)) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if !authKeyVerification(r.Header.Get("AuthKey"), r.Header.Get("NationName")) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	handle(w, r, dbPool)
}

func securedPostLTWrapper(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool, fsClient *firestore.Client, handle func(http.ResponseWriter, *http.Request, *pgxpool.Pool, *firestore.Client)) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !authKeyVerification(r.Header.Get("AuthKey"), r.Header.Get("NationName")) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	handle(w, r, dbPool, fsClient)
}

func openGetLTWrapper(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool, fsClient *firestore.Client, handle func(http.ResponseWriter, *http.Request, *pgxpool.Pool, *firestore.Client)) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	handle(w, r, dbPool, fsClient)
}
