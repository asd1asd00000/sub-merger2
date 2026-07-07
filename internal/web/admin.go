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

	"github.com/asd1asd00000/sub-merger2/internal/api"
	"github.com/asd1asd00000/sub-merger2/internal/db"
	"github.com/asd1asd00000/sub-merger2/internal/models"
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

	nodeStatus := make(map[string]string)
	for _, node := range settings.Nodes {
		var token string
		var err error
		if node.PanelType == "marzban" {
			token, err = api.GetMarzbanToken(node.URL, node.Username, node.Password)
		} else {
			token, err = api.GetToken(node.URL, node.Username, node.Password)
		}
		
		if err != nil {
			nodeStatus[node.URL] = "🔴 Disconnected"
		} else if token != "" {
			nodeStatus[node.URL] = "🟢 Connected"
		}
	}

	tmpl, err := template.ParseFiles("templates/admin.html")
	if err != nil {
		http.Error(w, "❌ Template Parsing Error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	data := struct {
		Users      []UserItem
		Host       string
		Settings   models.SystemSettings
		NodeStatus map[string]string
	}{
		Users:      userList,
		Host:       r.Host,
		Settings:   settings,
		NodeStatus: nodeStatus,
	}
	
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "❌ Template Execution Error: "+err.Error(), http.StatusInternalServerError)
	}
}

func handleAddUser(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodPost {
		r.ParseForm()
		database, _ := db.LoadDB()
		settings, _ := db.LoadSettings()
		
		username := strings.TrimSpace(r.FormValue("username"))
		if username == "" {
			http.Error(w, "Username is required!", http.StatusBadRequest)
			return
		}

		volStr := strings.TrimSpace(r.FormValue("volume_limit"))
		volStr = strings.ReplaceAll(volStr, ",", ".")
		volumeGB, _ := strconv.ParseFloat(volStr, 64)
		volumeLimitBytes := int64(volumeGB * 1024 * 1024 * 1024)

		expireDays, _ := strconv.Atoi(r.FormValue("expire_days"))
		var expireTimestamp int64 = 0
		if expireDays > 0 {
			expireTimestamp = time.Now().AddDate(0, 0, expireDays).Unix()
		}

		numNodes := int64(len(settings.Nodes))
		if numNodes == 0 { numNodes = 1 }
		nodeVolumeLimit := volumeLimitBytes * numNodes

		var automaticallyGeneratedURLs []string

		// 🎯 ارتقا: پاس دادن پارامتر expireTimestamp به هر دو پنل
		for i, node := range settings.Nodes {
			var token, subLink string
			var err error
			nodeUsername := fmt.Sprintf("%s_%d", username, i+1)

			if node.PanelType == "marzban" {
				token, err = api.GetMarzbanToken(node.URL, node.Username, node.Password)
				if err == nil {
					subLink, err = api.CreateMarzbanSubscription(node.URL, token, nodeUsername, nodeVolumeLimit, expireTimestamp)
				}
			} else {
				token, err = api.GetToken(node.URL, node.Username, node.Password)
				if err == nil {
					subLink, err = api.CreateSubscription(node.URL, token, nodeUsername, nodeVolumeLimit, expireTimestamp)
				}
			}

			if err == nil && subLink != "" {
				automaticallyGeneratedURLs = append(automaticallyGeneratedURLs, subLink)
			}
		}

		if len(automaticallyGeneratedURLs) == 0 {
			automaticallyGeneratedURLs = r.Form["urls"]
		}

		newID := generateUUID()
		database[newID] = models.User{
			Username:    username,
			URLs:        automaticallyGeneratedURLs,
			VolumeLimit: volumeLimitBytes,
			ExpireAt:    expireTimestamp,
			CreatedAt:   time.Now().Unix(),
		}

		db.SaveDB(database)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	tmpl, _ := template.ParseFiles("templates/user_form.html")
	emptyUser := models.User{URLs: []string{""}}
	tmpl.Execute(w, map[string]interface{}{"User": emptyUser, "VolumeGB": 0, "ExpireDays": 0})
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

		if username != "" {
			user.Username = username
		}
		
		volStr := strings.TrimSpace(r.FormValue("volume_limit"))
		volStr = strings.ReplaceAll(volStr, ",", ".")
		volumeGB, _ := strconv.ParseFloat(volStr, 64)
		user.VolumeLimit = int64(volumeGB * 1024 * 1024 * 1024)
		
		expireDays, _ := strconv.Atoi(r.FormValue("expire_days"))
		if expireDays > 0 {
			user.ExpireAt = time.Now().AddDate(0, 0, expireDays).Unix()
		} else {
			user.ExpireAt = 0
		}
		
		user.URLs = urls
		
		database[userID] = user
		db.SaveDB(database)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	volumeGB := float64(user.VolumeLimit) / (1024 * 1024 * 1024)
	var remainingDays int64 = 0
	if user.ExpireAt > 0 {
		remainingDays = (user.ExpireAt - time.Now().Unix()) / 86400
		if remainingDays < 0 { remainingDays = 0 }
	}

	tmpl, _ := template.ParseFiles("templates/user_form.html")
	tmpl.Execute(w, map[string]interface{}{"User": user, "VolumeGB": volumeGB, "ExpireDays": remainingDays})
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
		if backupInterval < 1 { backupInterval = 1 }

		var nodes []models.Node
		for i := 1; i <= 3; i++ {
			nType := strings.TrimSpace(r.FormValue(fmt.Sprintf("node_type_%d", i)))
			if nType == "" { nType = "guardcore" }
			nUrl := strings.TrimSpace(r.FormValue(fmt.Sprintf("node_url_%d", i)))
			nUrl = strings.TrimRight(nUrl, "/")
			nUser := strings.TrimSpace(r.FormValue(fmt.Sprintf("node_user_%d", i)))
			nPass := strings.TrimSpace(r.FormValue(fmt.Sprintf("node_pass_%d", i)))
			
			if nUrl != "" {
				nodes = append(nodes, models.Node{URL: nUrl, Username: nUser, Password: nPass, PanelType: nType})
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
			Nodes:            nodes,
		}
		
		db.SaveSettings(settings)
		go db.TriggerInitialSync(settings)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	}
}
