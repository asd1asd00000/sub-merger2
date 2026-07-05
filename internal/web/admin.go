package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/asd1asd00000/sub-merger/internal/db"
	"github.com/asd1asd00000/sub-merger/internal/fetcher"
	"github.com/asd1asd00000/sub-merger/internal/models"
	"github.com/asd1asd00000/sub-merger/internal/parser"
)

func checkAuth(r *http.Request) bool {
	cookie, err := r.Cookie("admin_session")
	return err == nil && cookie.Value == "logged_in"
}

func generateUUID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func autoDetectName(urls []string) string {
	htmlHeaders := map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
		"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
	}
	results := fetcher.FetchConcurrent(urls, htmlHeaders)
	for _, res := range results {
		if res.Error == nil {
			if name := parser.ExtractCleanTitle(res.Content); name != "" {
				return name
			}
		}
	}
	return "Unnamed User"
}

func handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.ParseForm()
		username := r.FormValue("username")
		password := r.FormValue("password")

		settings, _ := db.LoadSettings()

		if username == settings.AdminUsername && password == settings.AdminPassword {
			http.SetCookie(w, &http.Cookie{
				Name:     "admin_session",
				Value:    "logged_in",
				Path:     "/",
				HttpOnly: true,
				Expires:  time.Now().Add(24 * time.Hour),
			})
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
			return
		}
		
		tmpl, _ := template.ParseFiles("templates/login.html")
		tmpl.Execute(w, map[string]string{"Error": "Invalid username or password."})
		return
	}

	tmpl, _ := template.ParseFiles("templates/login.html")
	tmpl.Execute(w, nil)
}

func handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:    "admin_session",
		Value:   "",
		Path:    "/",
		Expires: time.Unix(0, 0),
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

type UserItem struct {
	ID   string
	User models.User
}

func handleAdminPanel(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	database, _ := db.LoadDB()
	settings, _ := db.LoadSettings()
	
	var userList []UserItem
	for id, u := range database {
		userList = append(userList, UserItem{ID: id, User: u})
	}

	sort.Slice(userList, func(i, j int) bool {
		return userList[i].User.CreatedAt > userList[j].User.CreatedAt
	})

	tmpl, _ := template.ParseFiles("templates/admin.html")
	
	data := struct {
		Users    []UserItem
		Host     string
		Settings models.SystemSettings
	}{
		Users:    userList,
		Host:     r.Host,
		Settings: settings,
	}
	tmpl.Execute(w, data)
}

func handleAddUser(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodPost {
		r.ParseForm()
		database, _ := db.LoadDB()
		
		username := strings.TrimSpace(r.FormValue("username"))
		urls := r.Form["urls"]

		if username == "" {
			username = autoDetectName(urls)
		}

		newID := generateUUID()
		database[newID] = models.User{
			Username:  username,
			URLs:      urls,
			CreatedAt: time.Now().Unix(),
		}

		db.SaveDB(database)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	tmpl, _ := template.ParseFiles("templates/user_form.html")
	emptyUser := models.User{URLs: []string{"", ""}}
	tmpl.Execute(w, map[string]models.User{"User": emptyUser})
}

func handleEditUser(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	userID := r.PathValue("id")
	database, _ := db.LoadDB()
	
	user, exists := database[userID]
	if !exists {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	if r.Method == http.MethodPost {
		r.ParseForm()
		username := strings.TrimSpace(r.FormValue("username"))
		urls := r.Form["urls"]

		if username == "" {
			username = autoDetectName(urls)
		}

		user.Username = username
		user.URLs = urls
		
		database[userID] = user
		db.SaveDB(database)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	tmpl, _ := template.ParseFiles("templates/user_form.html")
	tmpl.Execute(w, map[string]models.User{"User": user})
}

func handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	userID := r.PathValue("id")
	database, _ := db.LoadDB()
	
	if _, exists := database[userID]; exists {
		delete(database, userID)
		db.SaveDB(database)
	}
	
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func handleBackup(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	database, _ := db.LoadDB()
	w.Header().Set("Content-Disposition", "attachment; filename=sub_merger_backup.json")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(database)
}

func handleRestore(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodPost {
		file, _, err := r.FormFile("backup_file")
		if err == nil {
			defer file.Close()
			var database map[string]models.User
			if err := json.NewDecoder(file).Decode(&database); err == nil {
				db.SaveDB(database)
			}
		}
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	}
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodPost {
		r.ParseForm()
		
		adminUser := strings.TrimSpace(r.FormValue("admin_username"))
		adminPass := strings.TrimSpace(r.FormValue("admin_password"))

		if adminUser == "" || adminPass == "" {
			http.Error(w, "SECURITY ERROR: Username and Password cannot be empty!", http.StatusBadRequest)
			return
		}

		// پردازش تایمر بکاپ
		backupInterval, _ := strconv.Atoi(r.FormValue("backup_interval"))
		if backupInterval < 1 {
			backupInterval = 1 // حداقل ۱ ساعت
		}

		settings := models.SystemSettings{
			AdminUsername:    adminUser,
			AdminPassword:    adminPass,
			BackupInterval:   backupInterval,
			
			TelegramToken:    strings.TrimSpace(r.FormValue("telegram_token")),
			TelegramChatID:   strings.TrimSpace(r.FormValue("telegram_chat_id")),
			TelegramPassword: r.FormValue("telegram_password"),
			
			SmtpEmail:        strings.TrimSpace(r.FormValue("smtp_email")),
			SmtpPassword:     strings.TrimSpace(r.FormValue("smtp_password")),
			SmtpReceiver:     strings.TrimSpace(r.FormValue("smtp_receiver")),
			SmtpZipPassword:  strings.TrimSpace(r.FormValue("smtp_zip_password")),
			
			TutorialsURL:     strings.TrimSpace(r.FormValue("tutorials_url")),
			AnnouncementsURL: strings.TrimSpace(r.FormValue("announcements_url")),
		}
		
		db.SaveSettings(settings)
		go db.TriggerInitialSync(settings)

		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	}
}
