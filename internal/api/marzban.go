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

// 🤖 موتور هوشمند استخراج (پشتیبانی از آرایه پاسارگاد + دیکشنری مرزبان)
func GetMarzbanInbounds(nodeURL, token string) interface{} {
	baseURL := strings.TrimRight(nodeURL, "/")
	req, _ := http.NewRequest("GET", baseURL+"/api/inbounds", nil)
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Accept", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	// ۱. تلاش برای خواندن به صورت آرایه مسطح (معماری پاسارگاد)
	var flatArray []string
	if err := json.Unmarshal(bodyBytes, &flatArray); err == nil {
		log.Printf("🔍 Auto-Extracted Pasargad Flat Inbounds: %v", flatArray)
		return flatArray
	}

	// ۲. تلاش برای خواندن به صورت دیکشنری (معماری مرزبان استاندارد)
	var rawMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawMap); err == nil {
		cleanInbounds := make(map[string][]string)
		for protocol, val := range rawMap {
			if list, ok := val.([]interface{}); ok {
				var tags []string
				for _, item := range list {
					if inboundObj, isMap := item.(map[string]interface{}); isMap {
						if tag, hasTag := inboundObj["tag"].(string); hasTag && tag != "" {
							tags = append(tags, tag)
						}
					}
				}
				if len(tags) > 0 {
					cleanInbounds[protocol] = tags
				}
			}
		}
		log.Printf("🔍 Auto-Extracted Marzban Map Inbounds: %v", cleanInbounds)
		return cleanInbounds
	}

	log.Printf("⚠️ Could not decode inbounds. Data: %s", string(bodyBytes))
	return nil
}

// ساخت کاربر با تزریق هوشمند تمام گروه‌ها/سرویس‌ها
func CreateMarzbanSubscription(nodeURL, token, username string, nodeVolumeLimit int64, expireTimestamp int64) (string, error) {
	baseURL := strings.TrimRight(nodeURL, "/")
	inbounds := GetMarzbanInbounds(baseURL, token)

	payload := map[string]interface{}{
		"username":                  username,
		"data_limit":                nodeVolumeLimit,
		"expire":                    expireTimestamp,
		"data_limit_reset_strategy": "no_reset",
		"proxies": map[string]interface{}{
			"vmess":       map[string]interface{}{},
			"vless":       map[string]interface{}{},
			"trojan":      map[string]interface{}{},
			"shadowsocks": map[string]interface{}{},
		},
		"status": "active",
	}

	// اگر اینباندها با موفقیت استخراج شدند، آن‌ها را به بدنه اضافه کن
	if inbounds != nil {
		payload["inbounds"] = inbounds
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
