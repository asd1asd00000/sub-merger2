package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// دریافت توکن احراز هویت از GuardCore
func GetToken(nodeURL, username, password string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "password")
	data.Set("username", username)
	data.Set("password", password)

	req, err := http.NewRequest("POST", nodeURL+"/api/admins/token", strings.NewReader(data.Encode()))
	if err != nil { return "", err }
	
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get token, status: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	
	if token, ok := result["access_token"].(string); ok {
		return token, nil
	}
	return "", fmt.Errorf("token not found in response")
}

// خواندن مصرف کل کاربر از نود
func GetUserUsage(nodeURL, token, targetUser string) (int64, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/subscriptions/%s/usages", nodeURL, targetUser), nil)
	req.Header.Add("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return 0, err }
	defer resp.Body.Close()

	// پردازش هوشمند برای پیدا کردن حجم مصرفی (آپلود + دانلود)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	var totalUsed int64 = 0
	if usages, ok := result["usages"].([]interface{}); ok {
		for _, u := range usages {
			if usageMap, isMap := u.(map[string]interface{}); isMap {
				up, _ := usageMap["upload"].(float64)
				down, _ := usageMap["download"].(float64)
				totalUsed += int64(up + down)
			}
		}
	} else if used, ok := result["used_traffic"].(float64); ok { // ساختار جایگزین
		totalUsed = int64(used)
	}
	
	return totalUsed, nil
}

// مسدود کردن کاربر در نود (تیر خلاص)
func DisableUser(nodeURL, token, targetUser string) error {
	// استفاده از متد آپدیت و تغییر وضعیت به غیرفعال
	payload := map[string]string{"status": "disabled"}
	jsonData, _ := json.Marshal(payload)

	req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/subscriptions/%s", nodeURL, targetUser), bytes.NewBuffer(jsonData))
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()

	log.Printf("🛡️ Sent Disable command for user [%s] to node [%s] - Status: %d", targetUser, nodeURL, resp.StatusCode)
	return nil
}
