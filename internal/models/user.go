package models

// ساختار نودها (سرورهای اصلی GuardCore)
type Node struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ساختار دقیق کاربر با اضافه شدن محدودیت حجم
type User struct {
	Username    string   `json:"username"`
	URLs        []string `json:"urls"`
	CreatedAt   int64    `json:"created_at"`
	LastActive  int64    `json:"last_active"`
	VolumeLimit int64    `json:"volume_limit"` // حجم مجاز به بایت
}

// ساختار تنظیمات کل سیستم با اضافه شدن لیست نودها
type SystemSettings struct {
	AdminUsername    string `json:"admin_username"`
	AdminPassword    string `json:"admin_password"`
	
	BackupInterval   int    `json:"backup_interval"`
	
	TelegramToken    string `json:"token"`
	TelegramChatID   string `json:"chat_id"`
	TelegramPassword string `json:"password"`
	
	SmtpEmail        string `json:"smtp_email"`
	SmtpPassword     string `json:"smtp_password"`
	SmtpReceiver     string `json:"smtp_receiver"`
	SmtpZipPassword  string `json:"smtp_zip_password"`
	
	TutorialsURL     string `json:"tutorials_url"`
	AnnouncementsURL string `json:"announcements_url"`
	
	Nodes            []Node `json:"nodes"` // لیست نودهای متصل به سیستم
}
