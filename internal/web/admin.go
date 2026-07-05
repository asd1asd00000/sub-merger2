package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/asd1asd00000/sub-merger2/internal/db"
	"github.com/asd1asd00000/sub-merger2/internal/fetcher"
	"github.com/asd1asd00000/sub-merger2/internal/models"
	"github.com/asd1asd00000/sub-merger2/internal/parser"
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

		// تبدیل گیگابایت به بایت
		volumeGB, _ := strconv.ParseFloat(r.FormValue("volume_limit"), 64)
		volumeLimitBytes := int64(volumeGB * 1024 * 1024 * 1024)

		newID := generateUUID()
		database[newID] = models.User{
			Username:    username,
			URLs:        urls,
			VolumeLimit: volumeLimitBytes,
			CreatedAt:   time.Now().Unix(),
		}

		db.SaveDB(database)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	tmpl, _ := template.ParseFiles("templates/user_form.html")
	emptyUser := models.User{URLs: []string{"", ""}}
	// ارسال مقدار 0 به عنوان پیش‌فرض حجم در فرم جدید
	tmpl.Execute(w, map[string]interface{}{"User": emptyUser, "VolumeGB": 0})
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

		// تبدیل حجم دریافتی (گیگابایت) به بایت برای ذخیره
		volumeGB, _ := strconv.ParseFloat(r.FormValue("volume_limit"), 64)
		user.VolumeLimit = int64(volumeGB * 1024 * 1024 * 1024)

		user.Username = username
		user.URLs = urls
		
		database[userID] = user
		db.SaveDB(database)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	// تبدیل بایت به گیگابایت برای نمایش در فرم ویرایش
	volumeGB := float64(user.VolumeLimit) / (1024 * 1024 * 1024)

	tmpl, _ := template.ParseFiles("templates/user_form.html")
	tmpl.Execute(w, map[string]interface{}{"User": user, "VolumeGB": volumeGB})
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

		backupInterval, _ := strconv.Atoi(r.FormValue("backup_interval"))
		if backupInterval < 1 {
			backupInterval = 1
		}

		// پردازش و ذخیره نودهای GuardCore
		var nodes []models.Node
		for i := 1; i <= 3; i++ {
			nUrl := strings.TrimSpace(r.FormValue(fmt.Sprintf("node_url_%d", i)))
			nUser := strings.TrimSpace(r.FormValue(fmt.Sprintf("node_user_%d", i)))
			nPass := strings.TrimSpace(r.FormValue(fmt.Sprintf("node_pass_%d", i)))
			
			// پاک کردن اسلش انتهایی آدرس نود (برای جلوگیری از خطای API)
			nUrl = strings.TrimRight(nUrl, "/")
			
			if nUrl != "" {
				nodes = append(nodes, models.Node{URL: nUrl, Username: nUser, Password: nPass})
			}
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
			
			Nodes:            nodes, // اعمال نودها در سیستم
		}
		
		db.SaveSettings(settings)
		
		go db.TriggerInitialSync(settings)

		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	}
}
