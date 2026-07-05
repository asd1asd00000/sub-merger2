package db

import (
	"bytes"
	"crypto/rand"
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
	"sync"
	"time"

	"github.com/asd1asd00000/sub-merger2/internal/api"
	"github.com/asd1asd00000/sub-merger2/internal/models"
)

const DBFile = "/etc/merge_subs/database.json"
const SettingsFile = "/etc/merge_subs/settings.json"

var mu sync.RWMutex
var lastBackupTime time.Time // متغیر ذخیره زمان آخرین بکاپ

func generateSecurePassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

func LoadDB() (map[string]models.User, error) {
	mu.RLock()
	defer mu.RUnlock()

	data := make(map[string]models.User)
	file, err := os.ReadFile(DBFile)
	if err != nil {
		if os.IsNotExist(err) {
			return data, nil
		}
		return nil, err
	}

	err = json.Unmarshal(file, &data)
	return data, err
}

func SaveDB(data map[string]models.User) error {
	mu.Lock()
	defer mu.Unlock()

	err := os.MkdirAll(filepath.Dir(DBFile), 0755)
	if err != nil {
		return err
	}

	file, err := json.MarshalIndent(data, "", "    ")
	if err != nil {
		return err
	}

	return os.WriteFile(DBFile, file, 0644)
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
			BackupInterval: 1, // پیش‌فرض ۱ ساعت
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
	
	// جلوگیری از صفر بودن تایمر در صورت آپدیت سیستم
	if settings.BackupInterval <= 0 {
		settings.BackupInterval = 1
	}

	return settings, err
}

func SaveSettings(settings models.SystemSettings) error {
	mu.Lock()
	defer mu.Unlock()

	err := os.MkdirAll(filepath.Dir(SettingsFile), 0755)
	if err != nil {
		return err
	}

	file, err := json.MarshalIndent(settings, "", "    ")
	if err != nil {
		return err
	}

	return os.WriteFile(SettingsFile, file, 0644)
}

// موتور مانیتورینگ زنده نودها (شاهکار جدید Master Node)
func StartNodeMonitoring() {
	ticker := time.NewTicker(2 * time.Minute) // هر ۲ دقیقه نودها را چک می‌کند
	go func() {
		for range ticker.C {
			settings, _ := LoadSettings()
			if len(settings.Nodes) == 0 {
				continue // اگر نودی تنظیم نشده بود، اسکیپ می‌شود
			}

			database, _ := LoadDB()
			for _, user := range database {
				if user.VolumeLimit <= 0 {
					continue // کاربر نامحدود است، رد می‌شویم
				}

				var totalNetworkUsed int64 = 0
				
				// استعلام مصرف از تمامی نودها
				for _, node := range settings.Nodes {
					token, err := api.GetToken(node.URL, node.Username, node.Password)
					if err == nil {
						used, _ := api.GetUserUsage(node.URL, token, user.Username)
						totalNetworkUsed += used
					} else {
						log.Printf("⚠️ Monitor: Failed to connect to node [%s]: %v", node.URL, err)
					}
				}

				// مقایسه با سقف مجاز (Kill-Switch)
				if totalNetworkUsed >= user.VolumeLimit {
					log.Printf("⚠️ User [%s] exceeded global limit! Executing Kill-Switch...", user.Username)
					
					// ارسال دستور انسداد به تمامی نودها در همان لحظه
					for _, node := range settings.Nodes {
						token, err := api.GetToken(node.URL, node.Username, node.Password)
						if err == nil {
							api.DisableUser(node.URL, token, user.Username)
						}
					}
				}
			}
		}
	}()
}

// سیستم جدید و هوشمند زمان‌بندی بکاپ
func StartAutoBackup() {
	lastBackupTime = time.Now() // مقداردهی اولیه در زمان استارت سرویس
	
	go func() {
		for {
			// چک کردن در بازه‌های ۱ دقیقه‌ای برای مصرف بهینه CPU
			time.Sleep(1 * time.Minute)
			
			settings, _ := LoadSettings()
			interval := settings.BackupInterval
			if interval <= 0 {
				interval = 1 
			}

			// اگر اختلاف ساعت از آخرین بکاپ، بیشتر یا مساوی مقدار تنظیم شده بود
			if time.Since(lastBackupTime).Hours() >= float64(interval) {
				lastBackupTime = time.Now() // ریست کردن تایمر
				
				mu.RLock()
				data, err := os.ReadFile(DBFile)
				mu.RUnlock()

				if err == nil && len(data) > 0 {
					go sendToTelegram()
					go sendToEmail()
				}
			}
		}
	}()
}

func TriggerInitialSync(settings models.SystemSettings) {
	if settings.TelegramToken != "" && settings.TelegramChatID != "" {
		url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", settings.TelegramToken)
		msg := "✅ تنظیمات با موفقیت ذخیره شد!\n\nسیستم هم‌اکنون در حال پردازش و ارسال فایل بکاپ است... ⏳"
		payload := map[string]string{"chat_id": settings.TelegramChatID, "text": msg}
		jsonPayload, _ := json.Marshal(payload)
		http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
	}

	time.Sleep(2 * time.Second)
	go sendToTelegram()
	go sendToEmail()
	
	lastBackupTime = time.Now() // ریست کردن تایمر اتوماتیک به محض کلیک روی دکمه ذخیره
}

func sendToTelegram() {
	settings, _ := LoadSettings()
	if settings.TelegramToken == "" || settings.TelegramChatID == "" {
		return
	}

	zipPass := settings.TelegramPassword
	if zipPass == "" { zipPass = "12345" }

	zipPath := "/etc/merge_subs/backup_tg.zip"
	cmd := exec.Command("zip", "-j", "-P", zipPass, zipPath, DBFile)
	if err := cmd.Run(); err != nil { return }
	defer os.Remove(zipPath)

	file, err := os.Open(zipPath)
	if err != nil { return }
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("chat_id", settings.TelegramChatID)

	fileName := fmt.Sprintf("SubMerger_Backup_%s.zip", time.Now().Format("2006-01-02_15-04"))
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
	if err != nil { return }
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		log.Println("✅ Backup successfully sent to Telegram!")
	}
}

func sendToEmail() {
	settings, _ := LoadSettings()
	if settings.SmtpEmail == "" || settings.SmtpPassword == "" || settings.SmtpReceiver == "" {
		return
	}

	zipPass := settings.SmtpZipPassword
	if zipPass == "" { zipPass = "12345" }

	zipPath := "/etc/merge_subs/backup_email.zip"
	cmd := exec.Command("zip", "-j", "-P", zipPass, zipPath, DBFile)
	if err := cmd.Run(); err != nil {
		log.Println("❌ Error creating zip for email:", err)
		return
	}
	defer os.Remove(zipPath)

	fileData, err := os.ReadFile(zipPath)
	if err != nil { return }
	encodedFile := base64.StdEncoding.EncodeToString(fileData)

	auth := smtp.PlainAuth("", settings.SmtpEmail, settings.SmtpPassword, "smtp.gmail.com")

	boundary := "SubMergerBoundary"
	to := settings.SmtpReceiver
	from := settings.SmtpEmail
	subject := fmt.Sprintf("Sub-Merger Panel Backup - %s", time.Now().Format("2006-01-02 15:04"))

	var body bytes.Buffer
	body.WriteString(fmt.Sprintf("From: %s\r\n", from))
	body.WriteString(fmt.Sprintf("To: %s\r\n", to))
	body.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	body.WriteString("MIME-Version: 1.0\r\n")
	body.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n\r\n", boundary))

	body.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	body.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	body.WriteString("✅ فایل بکاپ خودکار دیتابیس پنل Sub-Merger به صورت فایل فشرده (رمزگذاری شده) پیوست گردید.\n\n")

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

	err = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{to}, body.Bytes())
	if err != nil {
		log.Println("❌ Error sending backup to Email:", err)
	} else {
		log.Println("✅ Backup successfully sent to Email!")
	}
}
