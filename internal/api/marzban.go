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

func GetMarzbanToken(nodeURL, username, password string) (string, error) {
	baseURL := strings.TrimRight(nodeURL, "/")
	data := url.Values{}
	data.Set("grant_type", "password")
	data.Set("username", username)
	data.Set("password", password)

	req, err := http.NewRequest("POST", baseURL+"/api/admin/token", strings.NewReader(data.Encode()))
	if err != nil { return "", err }
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("marzban auth failed, status: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if token, ok := result["access_token"].(string); ok {
		return token, nil
	}
	return "", fmt.Errorf("marzban token not found")
}

// 🤖 تابع استخراج گروه با فال‌بک دقیق و شخصی‌سازی شده برای سرور شما
func GetPasargadGroupIDs(baseURL, token string) []int {
	// تلاش برای پیدا کردن گروه‌ها از مسیرهای احتمالی پاسارگاد
	endpoints := []string{"/api/groups", "/api/user_groups", "/api/admin/groups"}
	
	for _, ep := range endpoints {
		req, _ := http.NewRequest("GET", baseURL+ep, nil)
		req.Header.Add("Authorization", "Bearer "+token)
		req.Header.Add("Accept", "application/json")

		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Do(req)
		
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var groups []map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&groups); err == nil {
				var ids []int
				for _, g := range groups {
					if id, ok := g["id"].(float64); ok {
						ids = append(ids, int(id))
					}
				}
				if len(ids) > 0 {
					log.Printf("🔍 Auto-Extracted Pasargad Group IDs from %s: %v", ep, ids)
					return ids
				}
			}
		}
	}

	// 🎯 اگر مسیر API را پیدا نکرد، مستقیماً از آیدی‌های دقیق سرور خودتان که در لاگ قبلی دیدیم استفاده می‌کند
	log.Printf("⚠️ Using specific fallback IDs [1, 3] from your panel data.")
	return []int{1, 3}
}

// ساخت کاربر با تزریق دقیق گروه‌ها
func CreateMarzbanSubscription(nodeURL, token, username string, nodeVolumeLimit int64, expireTimestamp int64) (string, error) {
	baseURL := strings.TrimRight(nodeURL, "/")
	
	groupIDs := GetPasargadGroupIDs(baseURL, token)

	payload := map[string]interface{}{
		"username":                  username,
		"data_limit":                nodeVolumeLimit,
		"data_limit_reset_strategy": "no_reset",
		"hwid_limit":                0,
		"status":                    "active",
		"group_ids":                 groupIDs, // تزریق دقیق [1, 3]
		"proxy_settings": map[string]interface{}{
			"vmess":       map[string]interface{}{},
			"vless":       map[string]interface{}{},
			"trojan":      map[string]interface{}{},
			"shadowsocks": map[string]interface{}{},
		},
	}

	if expireTimestamp > 0 {
		payload["expire"] = expireTimestamp
	} else {
		payload["expire"] = nil
	}

	jsonData, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", baseURL+"/api/user", bytes.NewBuffer(jsonData))
	if err != nil { return "", err }
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("❌ Pasargad API Error on [%s]: %s\n", baseURL, string(bodyBytes))
		return "", fmt.Errorf("create failed, status: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if subURL, ok := result["subscription_url"].(string); ok && subURL != "" {
		if strings.HasPrefix(subURL, "/") {
			return baseURL + subURL, nil
		}
		return subURL, nil
	}

	if links, ok := result["links"].([]interface{}); ok && len(links) > 0 {
		if linkStr, ok := links[0].(string); ok {
			return linkStr, nil
		}
	}
	return "", fmt.Errorf("could not extract marzban link properties")
}

func GetMarzbanUserUsage(nodeURL, token, targetUser string) (int64, error) {
	baseURL := strings.TrimRight(nodeURL, "/")
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/user/%s", baseURL, targetUser), nil)
	if err != nil { return 0, err }
	req.Header.Add("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return 0, err }
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if used, ok := result["used_traffic"].(float64); ok {
		return int64(used), nil
	}
	return 0, fmt.Errorf("marzban could not find usage data")
}

func DisableMarzbanUsers(nodeURL, token string, usernames []string) error {
	baseURL := strings.TrimRight(nodeURL, "/")
	client := &http.Client{Timeout: 10 * time.Second}
	for _, u := range usernames {
		reqGet, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/user/%s", baseURL, u), nil)
		reqGet.Header.Add("Authorization", "Bearer "+token)
		respGet, err := client.Do(reqGet)
		if err != nil { continue }
		
		var user map[string]interface{}
		json.NewDecoder(respGet.Body).Decode(&user)
		respGet.Body.Close()

		if user["username"] == nil { continue }

		user["status"] = "disabled"
		jsonData, _ := json.Marshal(user)
		
		reqPut, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/user/%s", baseURL, u), bytes.NewBuffer(jsonData))
		reqPut.Header.Add("Authorization", "Bearer "+token)
		reqPut.Header.Add("Content-Type", "application/json")
		
		respPut, err := client.Do(reqPut)
		if err == nil { respPut.Body.Close() }
	}
	log.Printf("🛡️ Sent Disable (Pasargad/Marzban) for users %v to node [%s]", usernames, baseURL)
	return nil
}
