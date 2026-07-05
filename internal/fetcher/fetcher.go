package fetcher

import (
	"io"
	"net/http"
	"sync"
	"time"
)

type SubResult struct {
	Index   int
	URL     string
	Content string
	Headers http.Header
	Error   error
}

func FetchConcurrent(urls []string, headers map[string]string) []SubResult {
	var wg sync.WaitGroup
	results := make([]SubResult, len(urls))

	for i, url := range urls {
		wg.Add(1)
		
		go func(index int, targetURL string) {
			defer wg.Done()
			
			client := &http.Client{Timeout: 12 * time.Second}
			req, err := http.NewRequest("GET", targetURL, nil)
			if err != nil {
				results[index] = SubResult{Index: index, URL: targetURL, Error: err}
				return
			}
			
			// تزریق هدرهای اختصاصی (داینامیک)
			for key, value := range headers {
				req.Header.Set(key, value)
			}

			resp, err := client.Do(req)
			if err != nil {
				results[index] = SubResult{Index: index, URL: targetURL, Error: err}
				return
			}
			defer resp.Body.Close()

			bodyBytes, _ := io.ReadAll(resp.Body)
			
			results[index] = SubResult{
				Index:   index,
				URL:     targetURL,
				Content: string(bodyBytes),
				Headers: resp.Header,
			}
		}(i, url)
	}

	wg.Wait()
	return results
}
