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
	return []int{1}
}

func CreateSubscription(nodeURL, token, username string, nodeVolumeLimit int64, expireTimestamp int64) (string, error) {
	serviceIDs := GetNodeServiceIDs(nodeURL, token)

	payload := []map[string]interface{}{
		{
			"username":     username,
			"limit_usage":  nodeVolumeLimit, 
			"limit_expire": expireTimestamp,
			"service_ids":  serviceIDs,
			"enabled":      true,
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
		if link, ok := firstResult["link"].(string); ok && link != "" {
			return link, nil
		}
		secret, _ := firstResult["secret"].(string)
		tag, _ := firstResult["tag"].(string)
		if secret != "" && tag != "" {
			return fmt.Sprintf("%s/%s/%s", strings.TrimRight(nodeURL, "/"), tag, secret), nil
		}
	}

	return "", fmt.Errorf("could not extract subscription link properties")
}

// 🎯 تابع جدید: ویرایش و روشن کردن مجدد کاربر در گاردکور
func UpdateSubscription(nodeURL, token, targetUser string, nodeVolumeLimit int64, expireTimestamp int64) error {
	baseURL := strings.TrimRight(nodeURL, "/")
	client := &http.Client{Timeout: 10 * time.Second}

	reqGet, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/subscriptions/%s", baseURL, targetUser), nil)
	reqGet.Header.Add("Authorization", "Bearer "+token)
	respGet, err := client.Do(reqGet)
	if err != nil { return err }
	defer respGet.Body.Close()

	if respGet.StatusCode != http.StatusOK {
		return fmt.Errorf("user not found on guardcore node")
	}

	var user map[string]interface{}
	json.NewDecoder(respGet.Body).Decode(&user)

	user["limit_usage"] = nodeVolumeLimit
	user["limit_expire"] = expireTimestamp
	user["enabled"] = true // روشن کردن مجدد!

	jsonData, _ := json.Marshal(user)
	reqPut, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/subscriptions/%s", baseURL, targetUser), bytes.NewBuffer(jsonData))
	reqPut.Header.Add("Authorization", "Bearer "+token)
	reqPut.Header.Add("Content-Type", "application/json")

	respPut, err := client.Do(reqPut)
	if err != nil { return err }
	defer respPut.Body.Close()

	if respPut.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(respPut.Body)
		return fmt.Errorf("guardcore update failed, status: %d, response: %s", respPut.StatusCode, string(bodyBytes))
	}
	return nil
}

func GetUserUsage(nodeURL, token, targetUser string) (int64, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/subscriptions/%s", nodeURL, targetUser), nil)
	req.Header.Add("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return 0, err }
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if current, ok := result["current_usage"].(float64); ok {
		return int64(current), nil
	} else if total, ok := result["total_usage"].(float64); ok {
		return int64(total), nil
	}
	
	return 0, fmt.Errorf("could not find usage data")
}

func DisableSubscriptions(nodeURL, token string, usernames []string) error {
	payload := map[string][]string{
		"usernames": usernames,
	}
	jsonData, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", nodeURL+"/api/subscriptions/disable", bytes.NewBuffer(jsonData))
	if err != nil { return err }
	
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()

	log.Printf("🛡️ Sent Bulk Disable (Kill-Switch) for users %v to node [%s] - Status: %d", usernames, nodeURL, resp.StatusCode)
	return nil
}
