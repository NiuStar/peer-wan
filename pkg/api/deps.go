package api

import (
	"net/http"
	"strings"

	"gorm.io/gorm"

	"peer-wan/pkg/auth"
)

var (
	dbRef       *gorm.DB
	wsHubGlobal *WSHub
)

func SetDB(db *gorm.DB) {
	dbRef = db
}

func authFuncJWT(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(h, "Bearer ")
	_, err := auth.Parse(token)
	return err == nil
}
