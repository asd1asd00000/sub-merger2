package db

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/asd1asd00000/sub-merger2/internal/api"
	"github.com/asd1asd00000/sub-merger2/internal/models"
	_ "github.com/go-sql-driver/mysql"
)

const SettingsFile = "/etc/merge_subs/settings.json"
const DBSecretFile = "/etc/merge_subs/.db_secret"

var mu sync.RWMutex
var lastBackupTime time.Time
var sqlDB *sql.DB

func init() {
	ConnectDB()
}

// اتصال به دیتابیس MariaDB و ساخت جدول کاربران
func ConnectDB() {
	passBytes, err := os.ReadFile(DBSecretFile)
	if err != nil {
		log.Printf("⚠️ Warning: Could not read db secret file. Is it installed correctly?")
		return
	}
	dbPass := strings.TrimSpace(string(passBytes))
	dsn := fmt.Sprintf("subadmin:%s@tcp(127.0.0.1:3306)/submerger?charset=utf8mb4&parseTime=True&loc=Local", dbPass)

	sqlDB, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("❌ Database Connection Error: %v", err)
	}

	err = sqlDB.Ping()
	if err != nil {
		log.Fatalf("❌ Database Ping Error: %v", err)
	}

	createTableQuery := `
	CREATE TABLE IF NOT EXISTS users (
		id VARCHAR(64) PRIMARY KEY,
		username VARCHAR(100) NOT NULL,
		urls JSON,
		created_at BIGINT,
		last_active BIGINT,
		volume_limit BIGINT,
		expire_at BIGINT,
		status VARCHAR(20) DEFAULT 'active'
	);`

	_, err = sqlDB.Exec(createTableQuery)
	if err != nil {
		log.Fatalf("❌ Database Table Creation Error: %v", err)
	}
	log.Println("✅ Connected to MariaDB Successfully!")
}

func generateSecurePassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// 🎯 خواندن اطلاعات از SQL
func LoadDB() (map[string]models.User, error) {
	if sqlDB == nil { return nil, fmt.Errorf("database not initialized") }
	data := make(map[string]models.User)

	rows, err := sqlDB.Query("SELECT id, username, urls, created_at, last_active, volume_limit, expire_at, status FROM users")
	if err != nil { return nil, err }
	defer rows.Close()

	for rows.Next() {
		var id string
		var user models.User
		var urlsJSON []byte

		err := rows.Scan(&id, &user.Username, &urlsJSON, &user.CreatedAt, &user.LastActive, &user.VolumeLimit, &user.ExpireAt, &user.Status)
		if err != nil { continue }

		json.Unmarshal(urlsJSON, &user.URLs)
		data[id] = user
	}
	return data, nil
}

// 🎯 ذخیره یا آپدیت کاربر در SQL (Upsert)
func SaveDB(data map[string]models.User) error {
	if sqlDB == nil { return fmt.Errorf("database not initialized") }

	for id, user := range data {
		urlsJSON, _ := json.Marshal(user.URLs)
		if user.Status == "" { user.Status = "active" } // پیش‌فرض

		query := `
			INSERT INTO users (id, username, urls, created_at, last_active, volume_limit, expire_at, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE 
			username=VALUES(username), urls=VALUES(urls), last_active=VALUES(last_active),
			volume_limit=VALUES(volume_limit), expire_at=VALUES(expire_at), status=VALUES(status)
		`
		_, err := sqlDB.Exec(query, id, user.Username, string(urlsJSON), user.CreatedAt, user.LastActive, user.VolumeLimit, user.ExpireAt, user.Status)
		if err != nil {
			log.Printf("❌ DB Save Error for user %s: %v", user.Username, err)
		}
	}
	return nil
}

// 🎯 پاک کردن کاربر از SQL
func DeleteUserDB(id string) error {
	if sqlDB == nil { return fmt.Errorf("database not initialized") }
	_, err := sqlDB.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

func LoadSettings() (models.SystemSettings, error) {
	mu.RLock()
	file, err := os.ReadFile(SettingsFile)
	mu.RUnlock()

	var settings models.SystemSettings

	if err != nil || len(file) == 0 {
		randomPass := generateSecurePassword(12)
		settings = models.SystemSettings{
			AdminUsername:  "admin",
			AdminPassword:  randomPass,
			BackupInterval: 1,
		}
		SaveSettings(settings)
		return settings, nil
	}

	err = json.Unmarshal(file, &settings)
	if settings.AdminUsername == "" || settings.AdminPassword == "" {
		settings.AdminUsername = "admin"
		settings.AdminPassword = generateSecurePassword(12)
		SaveSettings(settings)
	}
	
	if settings.BackupInterval <= 0 { settings.BackupInterval = 1 }

	return settings, err
}

func SaveSettings(settings models.SystemSettings) error {
	mu.Lock()
	defer mu.Unlock()

	err := os.MkdirAll(filepath.Dir(SettingsFile), 0755)
	if err != nil { return err }

	file, err := json.MarshalIndent(settings, "", "    ")
	if err != nil { return err }

	return os.WriteFile(SettingsFile, file, 0644)
}

// 🤖 موتور رادار (با قابلیت ثبت وضعیت قطعی در دیتابیس SQL)
func StartNodeMonitoring() {
	ticker := time.NewTicker(2 * time.Minute)
	go func() {
		for range ticker.C {
			settings, _ := LoadSettings()
			if len(settings.Nodes) == 0 || sqlDB == nil { continue }

			// 🎯 فقط کاربرانی که وضعیت active دارند را مانیتور کن
			rows, err := sqlDB.Query("SELECT id, username, volume_limit, expire_at FROM users WHERE status = 'active'")
			if err != nil { continue }

			var activeUsers []models.User
			var userIDs []string
			
			for rows.Next() {
				var id string
				var u models.User
				rows.Scan(&id, &u.Username, &u.VolumeLimit, &u.ExpireAt)
				activeUsers = append(activeUsers, u)
				userIDs = append(userIDs, id)
			}
			rows.Close()

			now := time.Now().Unix()

			for idx, user := range activeUsers {
				var shouldDisable = false
				
				if user.ExpireAt > 0 && now >= user.ExpireAt {
					log.Printf("⏳ User [%s] subscription expired! Triggering Kill-Switch...", user.Username)
					shouldDisable = true
				}

				if !shouldDisable && user.VolumeLimit > 0 {
					var totalNetworkUsed int64 = 0
					
					for i, node := range settings.Nodes {
						var token string
						var err error
						var used int64
						nodeUsername := fmt.Sprintf("%s_%d", user.Username, i+1)

						if node.PanelType == "marzban" {
							token, err = api.GetMarzbanToken(node.URL, node.Username, node.Password)
							if err == nil {
								used, _ = api.GetMarzbanUserUsage(node.URL, token, nodeUsername)
							}
						} else {
							token, err = api.GetToken(node.URL, node.Username, node.Password)
							if err == nil {
								used, _ = api.GetUserUsage(node.URL, token, nodeUsername)
							}
						}
						totalNetworkUsed += used
					}

					if totalNetworkUsed > 0 {
						log.Printf("📊 Live Radar -> User: [%s] | Used: %d bytes | Limit: %d bytes", user.Username, totalNetworkUsed, user.VolumeLimit)
					}

					if totalNetworkUsed >= user.VolumeLimit {
						log.Printf("🚨 MASTER KILL-SWITCH ACTIVATED! User [%s] exceeded global limit.", user.Username)
						shouldDisable = true
					}
				}

				if shouldDisable {
					// 🎯 ثبت فوری وضعیت قطعی در دیتابیس
					sqlDB.Exec("UPDATE users SET status = 'disabled' WHERE id = ?", userIDs[idx])

					for i, node := range settings.Nodes {
						nodeUsername := fmt.Sprintf("%s_%d", user.Username, i+1)
						if node.PanelType == "marzban" {
							token, err := api.GetMarzbanToken(node.URL, node.Username, node.Password)
							if err == nil {
								api.DisableMarzbanUsers(node.URL, token, []string{nodeUsername})
							}
						} else {
							token, err := api.GetToken(node.URL, node.Username, node.Password)
							if err == nil {
								api.DisableSubscriptions(node.URL, token, []string{nodeUsername})
							}
						}
					}
				}
			}
		}
	}()
}

// بکاپ‌گیری از SQL با استفاده از mysqldump
func dumpSQLDatabase(zipPath string, zipPass string) error {
	passBytes, _ := os.ReadFile(DBSecretFile)
	dbPass := strings.TrimSpace(string(passBytes))
	
	sqlFile := "/etc/merge_subs/backup.sql"
	cmd := exec.Command("mysqldump", "-u", "subadmin", "-p"+dbPass, "submerger")
	outfile, err := os.Create(sqlFile)
	if err != nil { return err }
	cmd.Stdout = outfile
	cmd.Run()
	outfile.Close()

	zipCmd := exec.Command("zip", "-j", "-P", zipPass, zipPath, sqlFile)
	err = zipCmd.Run()
	os.Remove(sqlFile) // پاک کردن فایل SQL بعد از زیپ شدن
	return err
}

func StartAutoBackup() {
	lastBackupTime = time.Now()
	go func() {
		for {
			time.Sleep(1 * time.Minute)
			settings, _ := LoadSettings()
			interval := settings.BackupInterval
			if interval <= 0 { interval = 1 }

			if time.Since(lastBackupTime).Hours() >= float64(interval) {
				lastBackupTime = time.Now()
				go sendToTelegram()
				go sendToEmail()
			}
		}
	}()
}

func TriggerInitialSync(settings models.SystemSettings) {
	if settings.TelegramToken != "" && settings.TelegramChatID != "" {
		url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", settings.TelegramToken)
		msg := "✅ تنظیمات با موفقیت ذخیره شد!\n\nسیستم هم‌اکنون در حال پردازش و ارسال فایل بکاپ دیتابیس ماریا دی‌بی است... ⏳"
		payload := map[string]string{"chat_id": settings.TelegramChatID, "text": msg}
		jsonPayload, _ := json.Marshal(payload)
		http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
	}
	time.Sleep(2 * time.Second)
	go sendToTelegram()
	go sendToEmail()
	lastBackupTime = time.Now()
}

func sendToTelegram() {
	settings, _ := LoadSettings()
	if settings.TelegramToken == "" || settings.TelegramChatID == "" { return }
	zipPass := settings.TelegramPassword
	if zipPass == "" { zipPass = "12345" }

	zipPath := "/etc/merge_subs/backup_tg.zip"
	if err := dumpSQLDatabase(zipPath, zipPass); err != nil { return }
	defer os.Remove(zipPath)

	file, err := os.Open(zipPath)
	if err != nil { return }
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("chat_id", settings.TelegramChatID)
	fileName := fmt.Sprintf("SubMerger_MariaDB_%s.zip", time.Now().Format("2006-01-02_15-04"))
	part, err := writer.CreateFormFile("document", fileName)
	if err != nil { return }
	io.Copy(part, file)
	writer.Close()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", settings.TelegramToken)
	req, err := http.NewRequest("POST", url, body)
	if err != nil { return }
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err == nil { defer resp.Body.Close() }
}

func sendToEmail() {
	settings, _ := LoadSettings()
	if settings.SmtpEmail == "" || settings.SmtpPassword == "" || settings.SmtpReceiver == "" { return }
	zipPass := settings.SmtpZipPassword
	if zipPass == "" { zipPass = "12345" }

	zipPath := "/etc/merge_subs/backup_email.zip"
	if err := dumpSQLDatabase(zipPath, zipPass); err != nil { return }
	defer os.Remove(zipPath)

	fileData, err := os.ReadFile(zipPath)
	if err != nil { return }
	encodedFile := base64.StdEncoding.EncodeToString(fileData)

	auth := smtp.PlainAuth("", settings.SmtpEmail, settings.SmtpPassword, "smtp.gmail.com")
	boundary := "SubMergerBoundary"
	to := settings.SmtpReceiver
	from := settings.SmtpEmail
	subject := fmt.Sprintf("Sub-Merger MariaDB Backup - %s", time.Now().Format("2006-01-02 15:04"))

	var body bytes.Buffer
	body.WriteString(fmt.Sprintf("From: %s\r\n", from))
	body.WriteString(fmt.Sprintf("To: %s\r\n", to))
	body.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	body.WriteString("MIME-Version: 1.0\r\n")
	body.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n\r\n", boundary))

	body.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	body.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	body.WriteString("✅ فایل بکاپ خودکار دیتابیس MariaDB (SQL) پیوست گردید.\n\n")

	body.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	body.WriteString("Content-Type: application/zip; name=\"backup.zip\"\r\n")
	body.WriteString("Content-Transfer-Encoding: base64\r\n")
	body.WriteString("Content-Disposition: attachment; filename=\"backup.zip\"\r\n\r\n")

	for i := 0; i < len(encodedFile); i += 76 {
		end := i + 76
		if end > len(encodedFile) { end = len(encodedFile) }
		body.WriteString(encodedFile[i:end] + "\r\n")
	}
	body.WriteString(fmt.Sprintf("\r\n--%s--\r\n", boundary))
	_ = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{to}, body.Bytes())
}
