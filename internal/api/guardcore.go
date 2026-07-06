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

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth failed, status: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	
	if token, ok := result["access_token"].(string); ok {
		return token, nil
	}
	return "", fmt.Errorf("token not found")
}

// 🤖 استخراج اتوماتیک لیست آیدی سرویس‌ها از نود
func GetNodeServiceIDs(nodeURL, token string) []int {
	req, _ := http.NewRequest("GET", nodeURL+"/api/services", nil)
	req.Header.Add("Authorization", "Bearer "+token)
	
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return []int{1} } 
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	
	var listResult []map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &listResult); err == nil {
		var ids []int
		for _, s := range listResult {
			if id, ok := s["id"].(float64); ok { ids = append(ids, int(id)) }
		}
		if len(ids) > 0 { return ids }
	}
	return []int{1} // مقدار زاپاس اگر هیچ سرویسی پیدا نشد
}

// ساخت مستقیم کاربر روی نود GuardCore (ترکیب فیلدهای صحیح + ساختار لیستی)
func CreateSubscription(nodeURL, token, username string, volumeLimitBytes int64) (string, error) {
	serviceIDs := GetNodeServiceIDs(nodeURL, token)

	// 🔥 توجه: اینجا از براکت [] استفاده شده تا Payload یک لیست معتبر (Array) باشد
	payload := []map[string]interface{}{
		{
			"username":     username,
			"limit_usage":  volumeLimitBytes,
			"limit_expire": 0,          // 0 یعنی بدون تاریخ انقضا
			"service_ids":  serviceIDs, // اختصاص سرویس‌ها
			"enabled":      true,       // طبق مستندات GET شما
		},
	}
	jsonData, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", nodeURL+"/api/subscriptions", bytes.NewBuffer(jsonData))
	if err != nil { return "", err }
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("❌ GuardCore API Error on [%s]: %s\n", nodeURL, string(bodyBytes))
		return "", fmt.Errorf("create failed, status: %d", resp.StatusCode)
	}

	var rawResult interface{}
	json.NewDecoder(resp.Body).Decode(&rawResult)

	var firstResult map[string]interface{}
	if listRes, ok := rawResult.([]interface{}); ok && len(listRes) > 0 {
		firstResult, _ = listRes[0].(map[string]interface{})
	} else if mapRes, ok := rawResult.(map[string]interface{}); ok {
		firstResult = mapRes
	}

	if firstResult != nil {
		// استفاده از فیلد مستقیم link طبق مستنداتی که فرستادید
		if link, ok := firstResult["link"].(string); ok && link != "" {
			return link, nil
		}
		// روش جایگزین
		secret, _ := firstResult["secret"].(string)
		tag, _ := firstResult["tag"].(string)
		if secret != "" && tag != "" {
			return fmt.Sprintf("%s/%s/%s", strings.TrimRight(nodeURL, "/"), tag, secret), nil
		}
	}

	return "", fmt.Errorf("could not extract subscription link properties")
}

// خواندن مصرف کل کاربر از نود
func GetUserUsage(nodeURL, token, targetUser string) (int64, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/subscriptions/%s/usages", nodeURL, targetUser), nil)
	req.Header.Add("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return 0, err }
	defer resp.Body.Close()

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
	} else if used, ok := result["used_traffic"].(float64); ok {
		totalUsed = int64(used)
	}
	
	return totalUsed, nil
}

// مسدود کردن کاربر در نود
func DisableUser(nodeURL, token, targetUser string) error {
	// طبق مستندات، پنل شما از enabled: false برای قطعی استفاده می‌کند
	payload := map[string]interface{}{"enabled": false}
	jsonData, _ := json.Marshal(payload)

	req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/subscriptions/%s", nodeURL, targetUser), bytes.NewBuffer(jsonData))
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()

	log.Printf("🛡️ Sent Disable (enabled:false) for user [%s] to node [%s]", targetUser, nodeURL)
	return nil
}
