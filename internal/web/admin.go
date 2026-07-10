package web

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
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
	ID          string
	User        models.User
	StatusText  string
	StatusColor string
}

func handleAdminPanel(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	database, _ := db.LoadDB()
	settings, _ := db.LoadSettings()
	
	var userList []UserItem
	now := time.Now().Unix()

	for id, u := range database {
		statusTxt := "🟢 Active"
		statusCol := "#10b981" 

		if u.Status == "disabled" {
			statusTxt = "🔴 Disabled (Limit)"
			statusCol = "#ef4444"
		} else if u.ExpireAt > 0 && u.ExpireAt < now {
			statusTxt = "🔴 Expired (Time)"
			statusCol = "#ef4444"
		}

		userList = append(userList, UserItem{
			ID:          id,
			User:        u,
			StatusText:  statusTxt,
			StatusColor: statusCol,
		})
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

	logCmd := exec.Command("journalctl", "-u", "sub-merger", "-n", "40", "--no-pager")
	logOut, err := logCmd.Output()
	logsStr := string(logOut)
	if err != nil || logsStr == "" {
		logsStr = "No logs available or no permission to read journalctl."
	}

	alertMsg := r.URL.Query().Get("msg")

	tmpl, err := template.ParseFiles("templates/admin.html")
	if err != nil {
		http.Error(w, "❌ Template Parsing Error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	data := struct {
		Users        []UserItem
		Host         string
		Settings     models.SystemSettings
		NodeStatus   map[string]string
		Logs         string
		AlertMessage string
	}{
		Users:        userList,
		Host:         r.Host,
		Settings:     settings,
		NodeStatus:   nodeStatus,
		Logs:         logsStr,
		AlertMessage: alertMsg,
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
		var resultMsgs []string 

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

			if err != nil {
				log.Printf("❌ Failed processing Node %d [%s]: %v", i+1, node.URL, err)
				resultMsgs = append(resultMsgs, fmt.Sprintf("❌ نود %d خطا", i+1))
			} else if subLink == "" {
				log.Printf("⚠️ Warning: Node %d [%s] returned an empty link for user %s", i+1, node.URL, username)
				resultMsgs = append(resultMsgs, fmt.Sprintf("⚠️ نود %d بدون لینک", i+1))
			} else {
				log.Printf("✅ Successfully extracted Link from Node %d [%s]: %s", i+1, node.URL, subLink)
				automaticallyGeneratedURLs = append(automaticallyGeneratedURLs, subLink)
				resultMsgs = append(resultMsgs, fmt.Sprintf("✅ نود %d موفق", i+1))
			}
		}

		if len(automaticallyGeneratedURLs) == 0 {
			automaticallyGeneratedURLs = r.Form["urls"]
		}

		newID := generateUUID()
		newUser := map[string]models.User{
			newID: {
				Username:    username,
				URLs:        automaticallyGeneratedURLs,
				VolumeLimit: volumeLimitBytes,
				ExpireAt:    expireTimestamp,
				CreatedAt:   time.Now().Unix(),
				Status:      "active",
			},
		}
		db.SaveDB(newUser)

		finalMsg := strings.Join(resultMsgs, " | ")
		if finalMsg == "" { finalMsg = "✅ عملیات ساخت با موفقیت انجام شد" }
		redirectURL := "/admin?msg=" + url.QueryEscape(finalMsg)
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
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
		
		newUsername := strings.TrimSpace(r.FormValue("username"))
		if newUsername == "" { newUsername = user.Username }
		
		volStr := strings.TrimSpace(r.FormValue("volume_limit"))
		volStr = strings.ReplaceAll(volStr, ",", ".")
		volumeGB, _ := strconv.ParseFloat(volStr, 64)
		newVolumeLimit := int64(volumeGB * 1024 * 1024 * 1024)
		
		expireDays, _ := strconv.Atoi(r.FormValue("expire_days"))
		var newExpireAt int64 = 0
		if expireDays > 0 {
			newExpireAt = time.Now().AddDate(0, 0, expireDays).Unix()
		}

		urlsRaw := r.Form["urls"]
		if len(urlsRaw) == 0 { urlsRaw = user.URLs }
		
		var cleanUrls []string
		for _, u := range urlsRaw {
			if strings.TrimSpace(u) != "" { cleanUrls = append(cleanUrls, u) }
		}

		oldUsername := user.Username

		settings, _ := db.LoadSettings()
		numNodes := int64(len(settings.Nodes))
		if numNodes == 0 { numNodes = 1 }
		nodeVolumeLimit := newVolumeLimit * numNodes

		var resultMsgs []string

		for i, node := range settings.Nodes {
			targetUser := fmt.Sprintf("%s_%d", oldUsername, i+1)
			var err error
			var newlyCreatedLink string

			if node.PanelType == "marzban" {
				token, _ := api.GetMarzbanToken(node.URL, node.Username, node.Password)
				if token != "" {
					newlyCreatedLink, err = api.UpdateMarzbanUser(node.URL, token, targetUser, nodeVolumeLimit, newExpireAt)
				} else { err = fmt.Errorf("auth failed") }
			} else {
				token, _ := api.GetToken(node.URL, node.Username, node.Password)
				if token != "" {
					newlyCreatedLink, err = api.UpdateSubscription(node.URL, token, targetUser, nodeVolumeLimit, newExpireAt)
				} else { err = fmt.Errorf("auth failed") }
			}

			if err != nil {
				log.Printf("❌ Failed to edit/activate user %s on node [%s]: %v", targetUser, node.URL, err)
				resultMsgs = append(resultMsgs, fmt.Sprintf("❌ نود %d خطا", i+1))
			} else {
				log.Printf("✅ User %s successfully edited & activated on node [%s]", targetUser, node.URL)
				resultMsgs = append(resultMsgs, fmt.Sprintf("✅ نود %d موفق", i+1))
				
				if newlyCreatedLink != "" {
					alreadyExists := false
					for _, u := range cleanUrls {
						if u == newlyCreatedLink { alreadyExists = true; break }
					}
					if !alreadyExists {
						cleanUrls = append(cleanUrls, newlyCreatedLink)
					}
				}
			}
		}

		user.Username = newUsername
		user.VolumeLimit = newVolumeLimit
		user.ExpireAt = newExpireAt
		user.URLs = cleanUrls
		user.Status = "active"
		
		db.SaveDB(map[string]models.User{userID: user}) 

		finalMsg := strings.Join(resultMsgs, " | ")
		redirectURL := "/admin?msg=" + url.QueryEscape(finalMsg)
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
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
	db.DeleteUserDB(userID) 
	
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// 🎯 تابع جدید تولید فایل زیپ بکاپ از دیتابیس MariaDB و فایل کانفیگ
func handleBackup(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	// استخراج رمز دیتابیس
	passBytes, err := os.ReadFile(db.DBSecretFile)
	if err != nil {
		http.Error(w, "Cannot read DB secret", http.StatusInternalServerError)
		return
	}
	dbPass := strings.TrimSpace(string(passBytes))

	// دامپ گرفتن از MariaDB
	sqlFile := "/tmp/backup.sql"
	cmd := exec.Command("mysqldump", "-u", "subadmin", "-p"+dbPass, "submerger")
	outfile, err := os.Create(sqlFile)
	if err == nil {
		cmd.Stdout = outfile
		cmd.Run()
		outfile.Close()
		defer os.Remove(sqlFile)
	}

	// ساخت فایل زیپ (ترکیب تنظیمات و دیتابیس)
	zipPath := fmt.Sprintf("/tmp/SubMerger_Full_Backup_%d.zip", time.Now().Unix())
	zipCmd := exec.Command("zip", "-j", zipPath, sqlFile, db.SettingsFile)
	zipCmd.Run()
	defer os.Remove(zipPath)

	w.Header().Set("Content-Disposition", "attachment; filename=SubMerger_Full_Backup.zip")
	w.Header().Set("Content-Type", "application/zip")
	http.ServeFile(w, r, zipPath)
}

// 🎯 تابع جدید ریستور هوشمند فایل زیپ 
func handleRestore(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodPost {
		file, _, err := r.FormFile("backup_file")
		if err == nil {
			defer file.Close()
			
			// ذخیره موقت فایل آپلودی
			tempZip := "/tmp/uploaded_restore.zip"
			out, err := os.Create(tempZip)
			if err == nil {
				io.Copy(out, file)
				out.Close()
				defer os.Remove(tempZip)

				// استخراج و بازگردانی محتویات
				zr, err := zip.OpenReader(tempZip)
				if err == nil {
					defer zr.Close()
					for _, f := range zr.File {
						if f.Name == "backup.sql" {
							// بازگردانی دیتابیس در MariaDB
							rc, _ := f.Open()
							sqlData, _ := io.ReadAll(rc)
							rc.Close()
							
							sqlPath := "/tmp/restore.sql"
							os.WriteFile(sqlPath, sqlData, 0644)
							
							passBytes, _ := os.ReadFile(db.DBSecretFile)
							dbPass := strings.TrimSpace(string(passBytes))
							
							cmd := exec.Command("mysql", "-u", "subadmin", "-p"+dbPass, "submerger")
							infile, _ := os.Open(sqlPath)
							cmd.Stdin = infile
							cmd.Run()
							infile.Close()
							os.Remove(sqlPath)
						} else if f.Name == "settings.json" {
							// بازگردانی فایل تنظیمات سیستم
							rc, _ := f.Open()
							settingsData, _ := io.ReadAll(rc)
							rc.Close()
							os.WriteFile(db.SettingsFile, settingsData, 0644)
						}
					}
				}
			}
		}
		
		redirectURL := "/admin?msg=" + url.QueryEscape("✅ بکاپ با موفقیت بازگردانی شد (تنظیمات + دیتابیس)")
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
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
