package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/asd1asd00000/sub-merger/internal/db"
	"github.com/asd1asd00000/sub-merger/internal/subscription"
)

func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /sub/", handleUserDashboard)

	mux.HandleFunc("GET /admin", handleAdminPanel)
	mux.HandleFunc("GET /admin/login", handleAdminLogin)
	mux.HandleFunc("POST /admin/login", handleAdminLogin)
	mux.HandleFunc("GET /admin/logout", handleAdminLogout)
	
	mux.HandleFunc("GET /admin/add", handleAddUser)
	mux.HandleFunc("POST /admin/add", handleAddUser)
	
	mux.HandleFunc("GET /admin/edit/{id}", handleEditUser)
	mux.HandleFunc("POST /admin/edit/{id}", handleEditUser)
	mux.HandleFunc("GET /admin/delete/{id}", handleDeleteUser)

	mux.HandleFunc("GET /admin/backup", handleBackup)
	mux.HandleFunc("POST /admin/restore", handleRestore)
	mux.HandleFunc("POST /admin/settings", handleSettings)

	mux.HandleFunc("GET /admin/users", handleGetUsers)
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "favicon.png")
	})
	mux.HandleFunc("GET /favicon.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "favicon.png")
	})
}

func handleUserDashboard(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimPrefix(r.URL.Path, "/sub/")
	if userID == "" {
		http.Error(w, "Invalid User ID", http.StatusBadRequest)
		return
	}

	database, err := db.LoadDB()
	if err != nil || len(database[userID].URLs) == 0 {
		http.Error(w, "User Not Found", http.StatusNotFound)
		return
	}

	// 🕒 ثبت و ذخیره زمان آخرین استفاده کاربر در دیتابیس به محض لود شدن لینک
	if user, exists := database[userID]; exists {
		user.LastActive = time.Now().Unix()
		database[userID] = user
		db.SaveDB(database)
	}

	data := subscription.ProcessUserData(userID, database[userID])

	ua := strings.ToLower(r.UserAgent())
	isBrowser := strings.Contains(ua, "mozilla") || strings.Contains(ua, "chrome") || strings.Contains(ua, "safari") || strings.Contains(ua, "edge") || strings.Contains(ua, "opera")

	if !isBrowser {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("subscription-userinfo", 
			fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", 
				data.TotalUp, data.TotalDl, data.TotalTot, data.TotalExp))
		w.Write([]byte(data.ConfigB64))
		return
	}

	tmpl, err := template.ParseFiles("templates/dashboard.html")
	if err != nil {
		http.Error(w, "Template Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "Render Error", http.StatusInternalServerError)
	}
}

func handleGetUsers(w http.ResponseWriter, r *http.Request) {
	database, _ := db.LoadDB()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(database)
}
