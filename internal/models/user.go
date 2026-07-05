package models

// ساختار دقیق کاربر
type User struct {
	Username   string   `json:"username"`
	URLs       []string `json:"urls"`
	CreatedAt  int64    `json:"created_at"`
	LastActive int64    `json:"last_active"`
}

// ساختار تنظیمات کل سیستم
type SystemSettings struct {
	AdminUsername    string `json:"admin_username"`
	AdminPassword    string `json:"admin_password"`
	
	BackupInterval   int    `json:"backup_interval"` // زمان بکاپ خودکار (به ساعت)
	
	TelegramToken    string `json:"token"`
	TelegramChatID   string `json:"chat_id"`
	TelegramPassword string `json:"password"`
	
	SmtpEmail        string `json:"smtp_email"`
	SmtpPassword     string `json:"smtp_password"`
	SmtpReceiver     string `json:"smtp_receiver"`
	SmtpZipPassword  string `json:"smtp_zip_password"` // رمز اختصاصی فایل فشرده ایمیل
	
	TutorialsURL     string `json:"tutorials_url"`
	AnnouncementsURL string `json:"announcements_url"`
}
