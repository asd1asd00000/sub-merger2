package subscription

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/asd1asd00000/sub-merger2/internal/db" // وارد کردن دیتابیس برای خواندن تنظیمات
	"github.com/asd1asd00000/sub-merger2/internal/fetcher"
	"github.com/asd1asd00000/sub-merger2/internal/models"
	"github.com/asd1asd00000/sub-merger2/internal/parser"
)

type PanelData struct {
	Title    string
	URL      string
	Exp      string
	UsedTxt  string
	TotalTxt string
	BG       string
	Index    int
	Percent  int
}

type DashboardData struct {
	CustomUsername   string
	StatusText       string
	StatusColor      string
	CardBG           string
	AlarmDisplay     string
	TotalTxt         string
	UsedTxt          string
	RemTxt           string
	ExpDate          string
	Percent          int
	JSSafeConfigs    string
	PanelsData       []PanelData
	ConfigB64        string
	TotalUp          int64
	TotalDl          int64
	TotalTot         int64
	TotalExp         int64
	TutorialsURL     string // فیلد جدید در داشبورد
	AnnouncementsURL string // فیلد جدید در داشبورد
}

var (
	upRegex  = regexp.MustCompile(`upload=(\d+)`)
	dlRegex  = regexp.MustCompile(`download=(\d+)`)
	totRegex = regexp.MustCompile(`total=(\d+)`)
	expRegex = regexp.MustCompile(`expire=(\d+)`)
)

func formatBytes(bytesVal int64) string {
	if bytesVal >= 1073741824 {
		return fmt.Sprintf("GB %.2f", float64(bytesVal)/1073741824)
	} else if bytesVal >= 1048576 {
		return fmt.Sprintf("MB %.2f", float64(bytesVal)/1048576)
	}
	return fmt.Sprintf("KB %.2f", float64(bytesVal)/1024)
}

func extractHeaderVal(regex *regexp.Regexp, info string) int64 {
	matches := regex.FindStringSubmatch(info)
	if len(matches) > 1 {
		val, _ := strconv.ParseInt(matches[1], 10, 64)
		return val
	}
	return 0
}

func ProcessUserData(userID string, user models.User) DashboardData {
	customUsername := strings.TrimSpace(user.Username)
	detectedName := ""

	if customUsername == "" && len(user.URLs) > 0 {
		htmlHeaders := map[string]string{
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
			"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
		}
		
		htmlResults := fetcher.FetchConcurrent(user.URLs, htmlHeaders)
		for _, res := range htmlResults {
			if res.Error == nil {
				if name := parser.ExtractCleanTitle(res.Content); name != "" {
					detectedName = name
					break
				}
			}
		}
	}

	finalUsername := customUsername
	if finalUsername == "" {
		if detectedName != "" {
			finalUsername = detectedName
		} else {
			finalUsername = "کاربر " + userID[:8]
		}
	}

	configHeaders := map[string]string{
		"User-Agent": "v2rayNG",
	}
	configResults := fetcher.FetchConcurrent(user.URLs, configHeaders)

	var totalUp, totalDl, totalTot int64
	var expires []int64
	var allConfigsRaw []string
	var panelsData []PanelData

	for i, res := range configResults {
		if res.Error != nil {
			panelsData = append(panelsData, PanelData{
				Title:    fmt.Sprintf("پنل %c", 65+i),
				URL:      res.URL,
				Exp:      "نامشخص",
				UsedTxt:  "0 MB",
				TotalTxt: "0 MB",
				BG:       "#7f1d1d",
				Index:    i,
				Percent:  0,
			})
			continue
		}

		rawConfigs := parser.DecodeConfigs(res.Content)
		allConfigsRaw = append(allConfigsRaw, rawConfigs)

		subInfo := res.Headers.Get("subscription-userinfo")
		up := extractHeaderVal(upRegex, subInfo)
		dl := extractHeaderVal(dlRegex, subInfo)
		tot := extractHeaderVal(totRegex, subInfo)
		exp := extractHeaderVal(expRegex, subInfo)

		totalUp += up
		totalDl += dl
		totalTot += tot
		if exp > 0 {
			expires = append(expires, exp)
		}

		title := fmt.Sprintf("پنل %c", 65+i)

		used := up + dl
		pBg := "#1f2937"
		if (tot > 0 && used >= tot) || (exp > 0 && time.Now().Unix() > exp) {
			pBg = "#7f1d1d"
		}

		pExpStr := "نامحدود"
		if exp > 0 {
			pExpStr = time.Unix(exp, 0).Format("2006-01-02")
		}

		pPercent := 0
		if tot > 0 {
			pPercent = int((float64(used) * 100) / float64(tot))
			if pPercent > 100 {
				pPercent = 100
			}
		}

		panelsData = append(panelsData, PanelData{
			Title:    title,
			URL:      res.URL,
			Exp:      pExpStr,
			UsedTxt:  formatBytes(used),
			TotalTxt: formatBytes(tot),
			BG:       pBg,
			Index:    i,
			Percent:  pPercent,
		})
	}

	usedBytes := totalUp + totalDl
	remBytes := int64(0)
	if totalTot-usedBytes > 0 {
		remBytes = totalTot - usedBytes
	}

	totalExp := int64(0)
	if len(expires) > 0 {
		totalExp = expires[0]
		for _, e := range expires {
			if e > totalExp {
				totalExp = e
			}
		}
	}

	percent := 0
	if totalTot > 0 {
		percent = int((float64(usedBytes) * 100) / float64(totalTot))
		if percent > 100 {
			percent = 100
		}
	}

	statusText, statusColor, cardBg, alarmDisplay := "✔️ فعال", "#10b981", "#111827", "none"
	if (totalTot > 0 && usedBytes >= totalTot) || (totalExp > 0 && time.Now().Unix() > totalExp) {
		statusText, statusColor, cardBg, alarmDisplay = "⚠️ غیرفعال", "#f59e0b", "#4c0519", "block"
	}

	expDate := "نامحدود"
	if totalExp > 0 {
		expDate = time.Unix(totalExp, 0).Format("2006-01-02 15:04")
	}

	allConfigsMerged := strings.Join(allConfigsRaw, "\n")
	configB64 := base64.StdEncoding.EncodeToString([]byte(allConfigsMerged))
	jsSafeConfigs := strings.ReplaceAll(allConfigsMerged, "`", "\\`")

	// بارگذاری لینک‌های داینامیک وبلاگ از تنظیمات سیستم
	settings, _ := db.LoadSettings()

	return DashboardData{
		CustomUsername:   finalUsername,
		StatusText:       statusText,
		StatusColor:      statusColor,
		CardBG:           cardBg,
		AlarmDisplay:     alarmDisplay,
		TotalTxt:         formatBytes(totalTot),
		UsedTxt:          formatBytes(usedBytes),
		RemTxt:           formatBytes(remBytes),
		ExpDate:          expDate,
		Percent:          percent,
		JSSafeConfigs:    jsSafeConfigs,
		PanelsData:       panelsData,
		ConfigB64:        configB64,
		TotalUp:          totalUp,
		TotalDl:          totalDl,
		TotalTot:         totalTot,
		TotalExp:         totalExp,
		TutorialsURL:     settings.TutorialsURL,     // انتساب لینک آموزش
		AnnouncementsURL: settings.AnnouncementsURL, // انتساب لینک اطلاعیه
	}
}
