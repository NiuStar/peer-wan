package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"peer-wan/pkg/auth"
	"peer-wan/pkg/model"
)

type AuthHandler struct {
	DB *gorm.DB
}

type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *AuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/auth/register", a.handleRegister)
	mux.HandleFunc("/api/v1/auth/login", a.handleLogin)
	mux.HandleFunc("/api/v1/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]int64{"version": 0})
	})
}

// handleRegister only allows the first user to be created (admin).
func (a *AuthHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	var count int64
	a.DB.Model(&model.User{}).Count(&count)
	if count > 0 {
		http.Error(w, "registration closed", http.StatusForbidden)
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	user := model.User{Username: req.Username, PasswordHash: string(hash), IsAdmin: true}
	if err := a.DB.Create(&user).Error; err != nil {
		http.Error(w, "failed to create user", http.StatusInternalServerError)
		return
	}
	token, _ := auth.Generate(user.ID, user.Username, 24*time.Hour)
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (a *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	var user model.User
	if err := a.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	token, _ := auth.Generate(user.ID, user.Username, 24*time.Hour)
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func AuthMiddleware(next func(http.ResponseWriter, *http.Request), requireJWT bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireJWT {
			next(w, r)
			return
		}
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(h, "Bearer ")
		if _, err := auth.Parse(token); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
