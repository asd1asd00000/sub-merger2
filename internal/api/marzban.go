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
	data := url.Values{}
	data.Set("grant_type", "password")
	data.Set("username", username)
	data.Set("password", password)

	req, err := http.NewRequest("POST", nodeURL+"/api/admin/token", strings.NewReader(data.Encode()))
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

// 🤖 موتور تصفیه هوشمند: تبدیل آبجکت‌های اینباند به لیست رشته‌ای از Tagها طبق استاندارد مرزبان
func GetMarzbanInbounds(nodeURL, token string) map[string][]string {
	req, _ := http.NewRequest("GET", nodeURL+"/api/inbounds", nil)
	req.Header.Add("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return map[string][]string{}
	}
	defer resp.Body.Close()

	// دریافت ساختار خام آبجکت‌های درون اینباندها
	var rawInbounds map[string][]map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawInbounds); err != nil {
		return map[string][]string{}
	}

	// استخراج بایت به بایت تگ‌ها و تبدیل به آرایه‌ای از رشته‌ها (map[string][]string)
	cleanInbounds := make(map[string][]string)
	for protocol, list := range rawInbounds {
		var tags []string
		for _, inbound := range list {
			if tag, ok := inbound["tag"].(string); ok && tag != "" {
				tags = append(tags, tag)
			}
		}
		if len(tags) > 0 {
			cleanInbounds[protocol] = tags
		}
	}
	return cleanInbounds
}

// ساخت کاربر با اعمال تاریخ انقضا و تیک زدن تمام سرویس‌ها و گروه‌ها به صورت خودکار
func CreateMarzbanSubscription(nodeURL, token, username string, nodeVolumeLimit int64, expireTimestamp int64) (string, error) {
	// دریافت لیست تگ‌های تصفیه شده اختصاصی نود
	inbounds := GetMarzbanInbounds(nodeURL, token)

	payload := map[string]interface{}{
		"username":                  username,
		"data_limit":                nodeVolumeLimit,
		"expire":                    expireTimestamp, // اعمال دقیق زمان انقضا روی نود
		"data_limit_reset_strategy": "no_reset",
		"proxies": map[string]interface{}{
			"vmess":       map[string]interface{}{},
			"vless":       map[string]interface{}{},
			"trojan":      map[string]interface{}{},
			"shadowsocks": map[string]interface{}{},
		},
		"inbounds":                  inbounds, // ارسال نقشه تگ‌های تصفیه شده جهت فعال‌سازی گروه‌ها
		"status":                    "active",
	}
	jsonData, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", nodeURL+"/api/user", bytes.NewBuffer(jsonData))
	if err != nil { return "", err }
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("❌ Pasargad API Error on [%s]: %s\n", nodeURL, string(bodyBytes))
		return "", fmt.Errorf("create failed, status: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if subURL, ok := result["subscription_url"].(string); ok && subURL != "" {
		if strings.HasPrefix(subURL, "/") {
			return strings.TrimRight(nodeURL, "/") + subURL, nil
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
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/user/%s", nodeURL, targetUser), nil)
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
	client := &http.Client{Timeout: 10 * time.Second}
	for _, u := range usernames {
		reqGet, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/user/%s", nodeURL, u), nil)
		reqGet.Header.Add("Authorization", "Bearer "+token)
		respGet, err := client.Do(reqGet)
		if err != nil { continue }
		
		var user map[string]interface{}
		json.NewDecoder(respGet.Body).Decode(&user)
		respGet.Body.Close()

		if user["username"] == nil { continue }

		user["status"] = "disabled"
		jsonData, _ := json.Marshal(user)
		
		reqPut, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/user/%s", nodeURL, u), bytes.NewBuffer(jsonData))
		reqPut.Header.Add("Authorization", "Bearer "+token)
		reqPut.Header.Add("Content-Type", "application/json")
		
		respPut, err := client.Do(reqPut)
		if err == nil { respPut.Body.Close() }
	}
	log.Printf("🛡️ Sent Disable (Pasargad/Marzban) for users %v to node [%s]", usernames, nodeURL)
	return nil
}
