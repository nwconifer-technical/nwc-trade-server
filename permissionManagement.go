package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5"
)

func (Env env) updatePerm(w http.ResponseWriter, r *http.Request) {
	log.Println("Permissions update")
	requingNat := r.Header.Get("NationName")
	decoder := json.NewDecoder(r.Body)
	var received struct {
		NationName    string
		NewPermission string
	}
	err := decoder.Decode(&received)
	if err != nil || (received.NewPermission != "citizen" && received.NewPermission != "trader") {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	dbConn, err := Env.DBPool.Acquire(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer dbConn.Release()
	var requingPerm, region, recingRegion, existingPerms string
	err = dbConn.QueryRow(r.Context(), `SELECT permission, region_name FROM nation_permissions WHERE nation_name = $1`, requingNat).Scan(&requingPerm, &region)
	if err != nil && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if requingPerm != "admin" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	err = dbConn.QueryRow(r.Context(), `SELECT region_name, permission FROM nation_permissions WHERE nation_name = $1`, received.NationName).Scan(&recingRegion, &existingPerms)
	if err != nil && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if region != recingRegion || existingPerms == "admin" {
		w.WriteHeader(http.StatusForbidden)
		log.Println("Incorrect Region or update an admin")
		return
	}
	if existingPerms == received.NewPermission {
		w.WriteHeader(http.StatusOK)
		return
	}
	err = dbConn.QueryRow(r.Context(), `UPDATE nation_permissions SET permission = $1 WHERE nation_name = $2`, received.NewPermission, received.NationName).Scan()
	if err != nil && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
