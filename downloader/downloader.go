package downloader

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
)

var tempDirMutex = &sync.Mutex{}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type SnapsaveResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Description string `json:"description,omitempty"`
		Preview     string `json:"preview,omitempty"`
		Media       []struct {
			URL        string `json:"url"`
			Thumbnail  string `json:"thumbnail,omitempty"`
			Type       string `json:"type"`
			Resolution string `json:"resolution,omitempty"`
		} `json:"media"`
	} `json:"data"`
}

type PlatformType string

const (
	Instagram PlatformType = "instagram"
	Twitter   PlatformType = "twitter"
	TikTok    PlatformType = "tiktok"
	Facebook  PlatformType = "facebook"
)

// decodeSnapApp расшифровывает данные согласно алгоритму snapsave
func decodeSnapApp(args []string) string {
	if len(args) < 6 {
		return ""
	}

	h, u, n, t, e, r := args[0], args[1], args[2], args[3], args[4], args[5]
	_ = u
	_ = r

	tNum, err := strconv.Atoi(t)
	if err != nil {
		return ""
	}

	eNum, err := strconv.Atoi(e)
	if err != nil {
		return ""
	}

	decode := func(d string, e, f int) string {
		g := strings.Split("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ+/", "")
		hArr := g[:e]
		iArr := g[:f]

		dChars := strings.Split(d, "")
		j := 0
		for c := 0; c < len(dChars); c++ {
			b := dChars[len(dChars)-1-c]
			idx := -1
			for i, char := range hArr {
				if char == b {
					idx = i
					break
				}
			}
			if idx != -1 {
				j += idx * int(math.Pow(float64(e), float64(c)))
			}
		}

		k := ""
		for j > 0 {
			k = iArr[j%f] + k
			j = int(math.Floor(float64(j) / float64(f)))
		}

		if k == "" {
			return "0"
		}
		return k
	}

	result := ""
	hLen := len(h)

	for i := 0; i < hLen; {
		s := ""
		for i < hLen && string(h[i]) != string(n[eNum]) {
			s += string(h[i])
			i++
		}
		i++

		for j := 0; j < len(n); j++ {
			s = strings.ReplaceAll(s, string(n[j]), strconv.Itoa(j))
		}

		decoded := decode(s, eNum, 10)
		decodedNum, err := strconv.Atoi(decoded)
		if err != nil {
			continue
		}
		charCode := decodedNum - tNum
		if charCode >= 0 && charCode <= 1114111 {
			result += string(rune(charCode))
		}
	}

	return fixEncoding(result)
}

func fixEncoding(str string) string {

	if utf8.ValidString(str) {
		return str
	}

	chars := []rune(str)
	bytes := make([]byte, 0, len(chars))

	for _, char := range chars {
		charCode := int(char)
		if charCode >= 0 && charCode <= 255 {
			bytes = append(bytes, byte(charCode))
		}
	}

	if utf8.Valid(bytes) {
		return string(bytes)
	}

	return str
}

// getEncodedSnapApp извлекает закодированные параметры из данных snapsave
func getEncodedSnapApp(data string) []string {
	parts := strings.Split(data, "decodeURIComponent(escape(r))}(")
	if len(parts) < 2 {

		if strings.Contains(data, "_0xe98c") {
			return tryExtractObfuscatedParams(data)
		}

		return nil
	}

	innerParts := strings.Split(parts[1], "))")
	if len(innerParts) < 1 {
		return nil
	}

	encoded := innerParts[0]

	commaParts := strings.Split(encoded, ",")
	result := make([]string, 0, len(commaParts))

	for _, part := range commaParts {
		cleaned := strings.ReplaceAll(strings.TrimSpace(part), "\"", "")
		result = append(result, cleaned)
	}

	return result
}

func tryExtractObfuscatedParams(data string) []string {
	return nil
}

func getDecodedSnapSave(data string) string {
	parts := strings.Split(data, "getElementById(\"download-section\").innerHTML = \"")
	if len(parts) < 2 {
		return ""
	}

	innerParts := strings.Split(parts[1], "\"; document.getElementById(\"inputData\").remove(); ")
	if len(innerParts) < 1 {
		return ""
	}

	result := innerParts[0]

	result = strings.ReplaceAll(result, "\\\\", "")
	result = strings.ReplaceAll(result, "\\", "")

	return result
}

func decryptSnapSave(data string) string {
	encoded := getEncodedSnapApp(data)
	if encoded == nil {
		return ""
	}
	decoded := decodeSnapApp(encoded)
	return getDecodedSnapSave(decoded)
}

func getDecodedSnaptik(data string) string {
	parts := strings.Split(data, "$(\"#download\").innerHTML = \"")
	if len(parts) < 2 {
		return ""
	}

	innerParts := strings.Split(parts[1], "\"; document.getElementById(\"inputData\").remove(); ")
	if len(innerParts) < 1 {
		return ""
	}

	result := innerParts[0]

	result = strings.ReplaceAll(result, "\\\\", "")
	result = strings.ReplaceAll(result, "\\", "")

	return result
}

func decryptSnaptik(data string) string {
	encoded := getEncodedSnapApp(data)
	if encoded == nil {
		return ""
	}
	decoded := decodeSnapApp(encoded)
	return getDecodedSnaptik(decoded)
}

func normalizeURL(url string) string {
	twitterRegex := regexp.MustCompile(`^https://(?:x|twitter)\.com(?:/(?:i/web|[^/]+)/status/(\d+)(?:.*)?)?$`)
	if twitterRegex.MatchString(url) {
		return url
	}

	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		if !strings.Contains(url, "://www.") {
			re := regexp.MustCompile(`^(https?://)([^./]+\.[^./]+)(\/.*)?$`)
			if re.MatchString(url) {
				return re.ReplaceAllString(url, "$1www.$2$3")
			}
		}
	}

	return url
}

func fixThumbnail(url string) string {
	toReplace := "https://snapinsta.app/photo.php?photo="
	if strings.Contains(url, toReplace) {
		decoded, err := neturl.QueryUnescape(strings.Replace(url, toReplace, "", 1))
		if err != nil {
			return url
		}
		return decoded
	}
	return url
}

func getUserAgent() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
}

func generateUniqueID() string {
	randomBytes := make([]byte, 8)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix()%10000)
	}
	return hex.EncodeToString(randomBytes)
}

func detectPlatform(mediaURL string) PlatformType {
	lowerURL := strings.ToLower(mediaURL)
	switch {
	case strings.Contains(lowerURL, "instagram.com"):
		return Instagram
	case strings.Contains(lowerURL, "twitter.com") || strings.Contains(lowerURL, "x.com"):
		return Twitter
	case strings.Contains(lowerURL, "tiktok.com"):
		return TikTok
	case strings.Contains(lowerURL, "facebook.com") || strings.Contains(lowerURL, "fb.watch"):
		return Facebook
	default:
		return Instagram
	}
}

func snapsaveDownload(mediaURL string, userID int64) (string, error) {
	platform := detectPlatform(mediaURL)

	outputPath, err := createUserDirectory(userID, string(platform))
	if err != nil {
		return "", err
	}

	videoURL, err := getSnapsaveVideoURL(mediaURL)
	if err != nil {
		return fallbackDownload(mediaURL, userID, platform)
	}

	return downloadMedia(videoURL, outputPath)
}

func getSnapsaveVideoURL(mediaURL string) (string, error) {
	platform := detectPlatform(mediaURL)

	switch platform {
	case TikTok:
		return getSnapsaveVideoURLTikTok(mediaURL)
	case Twitter:
		return getSnapsaveVideoURLTwitter(mediaURL)
	case Instagram, Facebook:
		return getSnapsaveVideoURLInstagramFacebook(mediaURL)
	default:
		return "", fmt.Errorf("неподдерживаемая платформа: %s", platform)
	}
}

func getSnapsaveVideoURLTikTok(mediaURL string) (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	homeReq, err := http.NewRequest("GET", "https://snaptik.app/", nil)
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса к snaptik.app: %v", err)
	}

	homeReq.Header.Set("User-Agent", getUserAgent())

	homeResp, err := client.Do(homeReq)
	if err != nil {
		return "", fmt.Errorf("ошибка запроса к snaptik.app: %v", err)
	}
	defer homeResp.Body.Close()

	if homeResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("неверный статус код от snaptik.app: %d", homeResp.StatusCode)
	}

	homeDoc, err := goquery.NewDocumentFromReader(homeResp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга HTML snaptik.app: %v", err)
	}

	token, exists := homeDoc.Find("input[name='token']").Attr("value")
	if !exists || token == "" {
		return "", fmt.Errorf("токен не найден на странице snaptik.app")
	}

	formData := neturl.Values{}
	formData.Set("url", mediaURL)
	formData.Set("token", token)

	postReq, err := http.NewRequest("POST", "https://snaptik.app/abc2.php", strings.NewReader(formData.Encode()))
	if err != nil {
		return "", fmt.Errorf("ошибка создания POST-запроса к snaptik.app: %v", err)
	}

	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("Accept", "*/*")
	postReq.Header.Set("Origin", "https://snaptik.app")
	postReq.Header.Set("Referer", "https://snaptik.app/")
	postReq.Header.Set("User-Agent", getUserAgent())

	postResp, err := client.Do(postReq)
	if err != nil {
		return "", fmt.Errorf("ошибка POST-запроса к snaptik.app: %v", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("неверный статус код от abc2.php: %d", postResp.StatusCode)
	}

	body, err := io.ReadAll(postResp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа от snaptik.app: %v", err)
	}

	decryptedHTML := decryptSnaptik(string(body))
	if decryptedHTML == "" {
		return "", fmt.Errorf("не удалось расшифровать данные snaptik")
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(decryptedHTML))
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга расшифрованного HTML snaptik: %v", err)
	}

	videoURL, exists := doc.Find(".download-box > .video-links > a").Attr("href")
	if !exists || videoURL == "" {
		videoURL, exists = doc.Find("a[download]").Attr("href")
		if !exists || videoURL == "" {
			videoURL, exists = doc.Find("a[href*='.mp4']").Attr("href")
			if !exists || videoURL == "" {
				return "", fmt.Errorf("видео URL не найден в ответе snaptik")
			}
		}
	}

	return videoURL, nil
}

func getSnapsaveVideoURLTwitter(mediaURL string) (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	homeReq, err := http.NewRequest("GET", "https://twitterdownloader.snapsave.app/", nil)
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса к twitterdownloader.snapsave.app: %v", err)
	}

	homeReq.Header.Set("User-Agent", getUserAgent())

	homeResp, err := client.Do(homeReq)
	if err != nil {
		return "", fmt.Errorf("ошибка запроса к twitterdownloader.snapsave.app: %v", err)
	}
	defer homeResp.Body.Close()

	if homeResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("неверный статус код от twitterdownloader.snapsave.app: %d", homeResp.StatusCode)
	}

	homeDoc, err := goquery.NewDocumentFromReader(homeResp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга HTML twitterdownloader.snapsave.app: %v", err)
	}

	token, exists := homeDoc.Find("input[name='token']").Attr("value")
	if !exists || token == "" {
		return "", fmt.Errorf("токен не найден на странице twitterdownloader.snapsave.app")
	}

	formData := neturl.Values{}
	formData.Set("url", mediaURL)
	formData.Set("token", token)

	postReq, err := http.NewRequest("POST", "https://twitterdownloader.snapsave.app/action.php", strings.NewReader(formData.Encode()))
	if err != nil {
		return "", fmt.Errorf("ошибка создания POST-запроса к twitterdownloader.snapsave.app: %v", err)
	}

	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("Accept", "*/*")
	postReq.Header.Set("Origin", "https://twitterdownloader.snapsave.app")
	postReq.Header.Set("Referer", "https://twitterdownloader.snapsave.app/")
	postReq.Header.Set("User-Agent", getUserAgent())

	postResp, err := client.Do(postReq)
	if err != nil {
		return "", fmt.Errorf("ошибка POST-запроса к twitterdownloader.snapsave.app: %v", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("неверный статус код от action.php: %d", postResp.StatusCode)
	}

	body, err := io.ReadAll(postResp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа от twitterdownloader.snapsave.app: %v", err)
	}

	var jsonResponse struct {
		Data string `json:"data"`
	}

	if err := json.Unmarshal(body, &jsonResponse); err != nil {
		return "", fmt.Errorf("ошибка парсинга JSON ответа: %v", err)
	}

	if jsonResponse.Data == "" {
		return "", fmt.Errorf("пустые данные в JSON ответе")
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(jsonResponse.Data))
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга HTML из JSON: %v", err)
	}

	videoURL, exists := doc.Find("#download-block > .abuttons > a").Attr("href")
	if !exists || videoURL == "" {
		return "", fmt.Errorf("видео URL не найден в ответе twitterdownloader")
	}

	return videoURL, nil
}

// getSnapsaveVideoURLInstagramFacebook получает URL видео для Instagram и Facebook
func getSnapsaveVideoURLInstagramFacebook(mediaURL string) (string, error) {
	apiURL := "https://snapsave.app/action.php?lang=en"

	formData := neturl.Values{}
	formData.Set("url", normalizeURL(mediaURL))

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", getUserAgent())
	req.Header.Set("Referer", "https://snapsave.app/")
	req.Header.Set("Origin", "https://snapsave.app")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения запроса: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("неверный статус код: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	decryptedHTML := decryptSnapSave(string(body))
	if decryptedHTML == "" {
		return findVideoURLWithRegex(string(body))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(decryptedHTML))
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга расшифрованного HTML: %v", err)
	}

	var videoURL string

	if doc.Find("table.table").Length() > 0 {
		doc.Find("tbody > tr").Each(func(i int, s *goquery.Selection) {
			td := s.Find("td")
			if td.Length() >= 3 {
				href, exists := td.Eq(2).Find("a").Attr("href")
				if exists && href != "" && videoURL == "" {
					videoURL = href
				} else {
					onclick, exists := td.Eq(2).Find("button").Attr("onclick")
					if exists && strings.Contains(onclick, "get_progressApi") {
						re := regexp.MustCompile(`get_progressApi\('([^']+)'\)`)
						matches := re.FindStringSubmatch(onclick)
						if len(matches) > 1 && videoURL == "" {
							videoURL = "https://snapsave.app" + matches[1]
						}
					}
				}
			}
		})
	}

	if videoURL == "" && doc.Find("div.card").Length() > 0 {
		doc.Find("div.card").Each(func(i int, s *goquery.Selection) {
			cardBody := s.Find("div.card-body")
			href, exists := cardBody.Find("a").Attr("href")
			if exists && href != "" && videoURL == "" {
				videoURL = href
			}
		})
	}

	if videoURL == "" && doc.Find("div.download-items").Length() > 0 {
		doc.Find("div.download-items").Each(func(i int, s *goquery.Selection) {
			itemBtn := s.Find("div.download-items__btn")
			href, exists := itemBtn.Find("a").Attr("href")
			if exists && href != "" && videoURL == "" {
				videoURL = href
			}
		})
	}

	if videoURL == "" {
		href, exists := doc.Find("a").Attr("href")
		if exists && href != "" {
			videoURL = href
		}
	}

	if videoURL == "" {
		return "", fmt.Errorf("не удалось найти видео URL в расшифрованном HTML")
	}

	return videoURL, nil
}

func findVideoURLWithRegex(htmlContent string) (string, error) {
	videoPatterns := []string{
		`href="([^"]*\.mp4[^"]*)"`,
		`data-href="([^"]*\.mp4[^"]*)"`,
		`onclick="[^"]*get_progressApi\('([^']+)'\)"`,
	}

	for _, pattern := range videoPatterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(htmlContent)
		if len(matches) > 1 {
			videoURL := matches[1]
			if strings.Contains(pattern, "get_progressApi") {
				videoURL = "https://snapsave.app" + videoURL
			}
			return videoURL, nil
		}
	}

	return "", fmt.Errorf("не удалось найти видео URL через регулярные выражения")
}

func createUserDirectory(userID int64, platform string) (string, error) {
	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}

	tempDirBase := filepath.Join(workDir, "temp_videos")

	tempDirMutex.Lock()
	defer tempDirMutex.Unlock()

	if err := os.MkdirAll(tempDirBase, 0755); err != nil {
		return "", fmt.Errorf("не удалось создать базовую директорию для временных файлов: %v", err)
	}

	userDir := filepath.Join(tempDirBase, strconv.FormatInt(userID, 10))
	if err := os.MkdirAll(userDir, 0755); err != nil {
		return "", fmt.Errorf("не удалось создать директорию пользователя для временных файлов: %v", err)
	}

	cacheDir := filepath.Join(tempDirBase, ".cache")
	configDir := filepath.Join(tempDirBase, ".config")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		fmt.Printf("Предупреждение: не удалось создать cache директорию: %v\n", err)
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		fmt.Printf("Предупреждение: не удалось создать config директорию: %v\n", err)
	}

	uniqueID := generateUniqueID()
	timestamp := time.Now().UnixNano()

	if platform == "youtube" {
		outputPath := filepath.Join(userDir, fmt.Sprintf("%s_%d_%s_%d.%%(ext)s", platform, userID, uniqueID, timestamp))
		return outputPath, nil
	}

	outputPath := filepath.Join(userDir, fmt.Sprintf("%s_%d_%s_%d.mp4", platform, userID, uniqueID, timestamp))
	return outputPath, nil
}

// fallbackDownload обрабатывает скачивание видео через fallback методы
func fallbackDownload(mediaURL string, userID int64, platform PlatformType) (string, error) {
	switch platform {
	case Instagram:
		return fallbackInstagramDownload(mediaURL, userID)
	case Twitter:
		return fallbackTwitterDownload(mediaURL, userID)
	case TikTok:
		return fallbackTikTokDownload(mediaURL, userID)
	case Facebook:
		return fallbackFacebookDownload(mediaURL, userID)
	default:
		return "", fmt.Errorf("платформа %s не поддерживается в fallback режиме", platform)
	}
}

func DownloadInstagramVideo(url string, userID int64) (string, error) {
	return snapsaveDownload(url, userID)
}

func DownloadTwitterVideo(url string, userID int64) (string, error) {
	return snapsaveDownload(url, userID)
}

func DownloadTikTokVideo(url string, userID int64) (string, error) {
	return snapsaveDownload(url, userID)
}

func DownloadFacebookVideo(url string, userID int64) (string, error) {
	return snapsaveDownload(url, userID)
}

func DownloadYouTubeVideo(url string, userID int64) (string, error) {
	outputPath, err := createUserDirectory(userID, "youtube")
	if err != nil {
		return "", fmt.Errorf("ошибка создания директории: %v", err)
	}

	if err := checkYtDlpAvailability(); err != nil {
		return "", fmt.Errorf("yt-dlp недоступен: %v", err)
	}

	args := []string{
		"--max-filesize", "50M",
		"--no-playlist",
		"--merge-output-format", "mp4",
		"--no-cache-dir",
		"--abort-on-error",
		"--output", outputPath,
		"--verbose",
		url,
	}

	cmd := exec.Command("yt-dlp", args...)

	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}
	cmd.Dir = workDir

	cmd.Env = append(os.Environ(),
		"XDG_CACHE_HOME="+filepath.Join(workDir, "temp_videos", ".cache"),
		"XDG_CONFIG_HOME="+filepath.Join(workDir, "temp_videos", ".config"),
		"HOME="+workDir,
	)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	timeout := 5 * time.Minute
	done := make(chan error, 1)

	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		stdoutStr := stdout.String()
		stderrStr := stderr.String()

		if err != nil {
			errorDetails := fmt.Sprintf("yt-dlp failed with exit code: %v\nStdout: %s\nStderr: %s\nCommand: %s",
				err, stdoutStr, stderrStr, strings.Join(append([]string{"yt-dlp"}, args...), " "))

			if strings.Contains(stderrStr, "Video unavailable") {
				return "", fmt.Errorf("видео недоступно (возможно, удалено или приватное)")
			}
			if strings.Contains(stderrStr, "Private video") {
				return "", fmt.Errorf("видео является приватным")
			}
			if strings.Contains(stderrStr, "Sign in to confirm your age") {
				return "", fmt.Errorf("видео имеет возрастные ограничения")
			}
			if strings.Contains(stderrStr, "This video is not available") {
				return "", fmt.Errorf("видео недоступно в вашем регионе")
			}
			if strings.Contains(stderrStr, "Requested format is not available") {
				return "", fmt.Errorf("запрашиваемый формат недоступен")
			}

			return "", fmt.Errorf("ошибка выполнения yt-dlp: %v\nДетали: %s", err, errorDetails)
		}

		if strings.Contains(stderrStr, "File is larger than max-filesize") {
			return "", fmt.Errorf("файл превышает ограничение размера (50MB)")
		}
		if strings.Contains(stderrStr, "Requested format is not available") {
			return "", fmt.Errorf("подходящий формат видео не найден (возможно, все версии слишком большие)")
		}
		if strings.Contains(stdoutStr, "has already been downloaded") || strings.Contains(stderrStr, "has already been downloaded") {
		}

		if strings.Contains(stdoutStr, "aborting") || strings.Contains(stderrStr, "aborting") {
			return "", fmt.Errorf("скачивание прервано (возможно, из-за превышения размера файла)")
		}

		fmt.Printf("yt-dlp успешно завершен.\nStdout: %s\nStderr: %s\nCommand: %s\n",
			stdoutStr, stderrStr, strings.Join(append([]string{"yt-dlp"}, args...), " "))

	case <-time.After(timeout):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return "", fmt.Errorf("превышен таймаут скачивания (5 минут)")
	}

	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		// yt-dlp может создать файл с другим именем, ищем все файлы в директории
		dir := filepath.Dir(outputPath)
		files, err := os.ReadDir(dir)
		if err != nil {
			return "", fmt.Errorf("файл не был создан и не удалось прочитать директорию: %v", err)
		}

		// Логируем содержимое директории для отладки
		var fileList []string
		for _, file := range files {
			if !file.IsDir() {
				fileList = append(fileList, file.Name())
			}
		}

		// Сначала ищем файлы с нашим базовым именем
		baseName := strings.TrimSuffix(filepath.Base(outputPath), ".mp4")
		for _, file := range files {
			if strings.HasPrefix(file.Name(), baseName) && !file.IsDir() {
				// Нашли файл с нашим базовым именем
				actualPath := filepath.Join(dir, file.Name())

				// Если это не mp4, переименовываем в mp4 для совместимости с Telegram
				if !strings.HasSuffix(file.Name(), ".mp4") {
					newPath := strings.TrimSuffix(actualPath, filepath.Ext(actualPath)) + ".mp4"
					if err := os.Rename(actualPath, newPath); err == nil {
						return newPath, nil
					}
				}
				return actualPath, nil
			}
		}

		// Очищаем .part файлы (незавершенные загрузки)
		partFilesFound := false
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".part") {
				partFilePath := filepath.Join(dir, file.Name())
				if err := os.Remove(partFilePath); err == nil {
					fmt.Printf("Удален незавершенный файл: %s\n", partFilePath)
					partFilesFound = true
				}
			}
		}

		// Если нашли .part файлы, это означает, что скачивание было прервано
		if partFilesFound {
			return "", fmt.Errorf("скачивание было прервано, созданы только частичные файлы (возможно, превышен размер или таймаут)")
		}

		// Если не нашли по базовому имени, ищем любые видео файлы, созданные недавно
		now := time.Now()
		for _, file := range files {
			if file.IsDir() {
				continue
			}

			// Проверяем видео расширения (исключая .part файлы)
			fileName := file.Name()
			if !strings.HasSuffix(fileName, ".part") &&
				(strings.HasSuffix(fileName, ".mp4") || strings.HasSuffix(fileName, ".webm") ||
					strings.HasSuffix(fileName, ".mkv") || strings.HasSuffix(fileName, ".avi")) {

				filePath := filepath.Join(dir, fileName)
				fileInfo, err := os.Stat(filePath)
				if err != nil {
					continue
				}

				// Если файл создан в последние 5 минут, считаем его нашим
				if now.Sub(fileInfo.ModTime()) < 5*time.Minute {
					// Переименовываем в ожидаемый формат
					newPath := strings.TrimSuffix(outputPath, ".%(ext)s") + ".mp4"
					if strings.Contains(outputPath, ".%(ext)s") {
						if err := os.Rename(filePath, newPath); err == nil {
							return newPath, nil
						}
					}
					return filePath, nil
				}
			}
		}

		return "", fmt.Errorf("файл не был создан после выполнения yt-dlp. Файлы в директории: %v", fileList)
	}

	// Проверяем размер файла
	fileInfo, err := os.Stat(outputPath)
	if err != nil {
		return "", fmt.Errorf("ошибка получения информации о файле: %v", err)
	}

	// Проверяем, что файл не слишком большой для Telegram (50MB лимит)
	const maxFileSize = 50 * 1024 * 1024 // 50MB
	if fileInfo.Size() > maxFileSize {
		os.Remove(outputPath) // Удаляем слишком большой файл
		return "", fmt.Errorf("файл слишком большой для отправки через Telegram (%.1f MB > 50 MB)", float64(fileInfo.Size())/(1024*1024))
	}

	// Проверяем, что файл не пустой
	if fileInfo.Size() < 1024 { // Минимум 1KB
		os.Remove(outputPath)
		return "", fmt.Errorf("скачанный файл слишком маленький (возможно, ошибка скачивания)")
	}

	return outputPath, nil
}

// checkYtDlpAvailability проверяет доступность yt-dlp
func checkYtDlpAvailability() error {
	cmd := exec.Command("yt-dlp", "--version")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("yt-dlp не установлен или недоступен: %v", err)
	}

	// Логируем версию для диагностики
	fmt.Printf("yt-dlp version: %s\n", strings.TrimSpace(string(output)))
	return nil
}

// fallbackInstagramDownload резервный метод для Instagram
func fallbackInstagramDownload(url string, userID int64) (string, error) {
	outputPath, err := createUserDirectory(userID, "instagram")
	if err != nil {
		return "", err
	}

	// Заменяем instagram.com на ddinstagram.com для легкого извлечения видео
	ddUrl := strings.Replace(url, "instagram.com", "ddinstagram.com", 1)

	// Настройка HTTP-клиента с увеличенным таймаутом
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("слишком много редиректов")
			}
			return nil
		},
	}

	// Отправка запроса к ddinstagram для получения HTML страницы
	req, err := http.NewRequest("GET", ddUrl, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка при создании запроса: %v", err)
	}

	// Устанавливаем заголовки для имитации TelegramBot
	req.Header.Set("User-Agent", "TelegramBot (like InstagramBot)")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка при запросе к ddinstagram: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("получен неверный статус код: %d", resp.StatusCode)
	}

	// Ищем видео URL через регулярные выражения в HTML
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	// Паттерны для поиска видео URL
	videoPatterns := []string{
		`"video_url":"([^"]+)"`,
		`og:video" content="([^"]+)"`,
		`twitter:player:stream" content="([^"]+)"`,
		`href="([^"]*\.mp4[^"]*)"`,
	}

	var videoURL string
	for _, pattern := range videoPatterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(string(body))
		if len(matches) > 1 {
			videoURL = matches[1]
			// Декодируем escape-последовательности
			videoURL = strings.ReplaceAll(videoURL, "\\u0026", "&")
			videoURL = strings.ReplaceAll(videoURL, "\\/", "/")
			break
		}
	}

	if videoURL == "" {
		return "", fmt.Errorf("не удалось найти URL видео в fallback режиме для Instagram")
	}

	return downloadMedia(videoURL, outputPath)
}

// fallbackTwitterDownload резервный метод для Twitter
func fallbackTwitterDownload(url string, userID int64) (string, error) {
	outputPath, err := createUserDirectory(userID, "twitter")
	if err != nil {
		return "", err
	}

	// Заменяем x.com на twitter.com, а затем twitter.com на vxtwitter.com
	url = strings.Replace(url, "x.com", "twitter.com", 1)
	vxUrl := strings.Replace(url, "twitter.com", "vxtwitter.com", 1)

	// Настройка HTTP-клиента
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("слишком много редиректов")
			}
			return nil
		},
	}

	// Отправка запроса к vxTwitter
	req, err := http.NewRequest("GET", vxUrl, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка при создании запроса: %v", err)
	}

	req.Header.Set("User-Agent", "TelegramBot (like TwitterBot)")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка при запросе к vxTwitter: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("получен неверный статус код: %d", resp.StatusCode)
	}

	// Ищем видео URL через регулярные выражения в HTML
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	// Паттерны для поиска видео URL в Twitter
	videoPatterns := []string{
		`twitter:player:stream" content="([^"]+)"`,
		`og:video" content="([^"]+)"`,
		`twitter:video" content="([^"]+)"`,
		`"video_url":"([^"]+)"`,
	}

	var videoURL string
	for _, pattern := range videoPatterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(string(body))
		if len(matches) > 1 {
			videoURL = matches[1]
			// Декодируем escape-последовательности
			videoURL = strings.ReplaceAll(videoURL, "\\u0026", "&")
			videoURL = strings.ReplaceAll(videoURL, "\\/", "/")
			break
		}
	}

	if videoURL == "" {
		return "", fmt.Errorf("не удалось найти URL видео в fallback режиме для Twitter")
	}

	return downloadMedia(videoURL, outputPath)
}

// fallbackTikTokDownload резервный метод для TikTok через tikmate.online
func fallbackTikTokDownload(url string, userID int64) (string, error) {

	// Создаем выходную директорию
	outputPath, err := createUserDirectory(userID, "tiktok")
	if err != nil {
		return "", err
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Отправляем POST-запрос к tikmate.online API
	formData := neturl.Values{}
	formData.Set("url", url)

	req, err := http.NewRequest("POST", "https://tikmate.online/download", strings.NewReader(formData.Encode()))
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса к tikmate.online: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", getUserAgent())
	req.Header.Set("Origin", "https://tikmate.online")
	req.Header.Set("Referer", "https://tikmate.online/")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка запроса к tikmate.online: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("неверный статус код от tikmate.online: %d", resp.StatusCode)
	}

	// Читаем ответ как JSON
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа от tikmate.online: %v", err)
	}

	// Парсим JSON ответ
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			VideoURL string `json:"play"`
		} `json:"data"`
	}

	err = json.Unmarshal(body, &response)
	if err != nil {
		// Если JSON не парсится, пробуем извлечь URL регулярными выражениями
		return fallbackTikTokRegexExtract(string(body), outputPath)
	}

	if !response.Success || response.Data.VideoURL == "" {
		return "", fmt.Errorf("tikmate.online не смог обработать URL")
	}

	return downloadMedia(response.Data.VideoURL, outputPath)
}

// fallbackTikTokRegexExtract извлекает URL видео регулярными выражениями
func fallbackTikTokRegexExtract(htmlContent string, outputPath string) (string, error) {
	// Ищем различные паттерны URL видео
	patterns := []string{
		`"play":"([^"]+)"`,
		`"video_url":"([^"]+)"`,
		`"download_url":"([^"]+)"`,
		`href="([^"]*\.mp4[^"]*)"`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(htmlContent)
		if len(matches) > 1 {
			videoURL := matches[1]
			// Декодируем URL если нужно
			videoURL = strings.ReplaceAll(videoURL, "\\u0026", "&")
			videoURL = strings.ReplaceAll(videoURL, "\\/", "/")

			return downloadMedia(videoURL, outputPath)
		}
	}

	return "", fmt.Errorf("не удалось найти URL видео в fallback режиме для TikTok")
}

// fallbackFacebookDownload резервный метод для Facebook (простой подход)
func fallbackFacebookDownload(url string, userID int64) (string, error) {
	// Для Facebook пока что просто возвращаем ошибку, так как fallback методы сложны
	// В будущем можно добавить альтернативные API
	return "", fmt.Errorf("facebook fallback метод пока не реализован - попробуйте позже")
}

// downloadMedia скачивает медиа по URL и сохраняет его в outputPath
func downloadMedia(url, outputPath string) (string, error) {
	// Удаляем лишние кавычки и экранированные символы в URL
	url = strings.Trim(url, "\"'")
	url = strings.ReplaceAll(url, "\\", "")

	// Проверяем и исправляем относительные URL
	if strings.HasPrefix(url, "/") {
		url = "https://ddinstagram.com" + url
	}

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("неверный URL формат: %s", url)
	}

	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("слишком много редиректов")
			}
			return nil
		},
	}

	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			lastErr = fmt.Errorf("ошибка при создании запроса для скачивания: %v", err)
			continue
		}

		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
		req.Header.Set("Accept", "video/mp4,video/webm,video/*;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Referer", "https://www.instagram.com/")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("ошибка при скачивании видео (попытка %d): %v", attempt+1, err)
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("получен неверный статус код при скачивании (попытка %d): %d", attempt+1, resp.StatusCode)
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}

		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "video/") && !strings.Contains(contentType, "application/octet-stream") && !strings.Contains(contentType, "binary/") {
			contentLength := resp.ContentLength
			if contentLength > 0 && contentLength < 10000 {
				lastErr = fmt.Errorf("контент не похож на видео: тип %s, размер %d байт", contentType, contentLength)
				continue
			}
		}

		out, err := os.Create(outputPath)
		if err != nil {
			return "", fmt.Errorf("ошибка при создании файла: %v", err)
		}

		n, err := io.Copy(out, resp.Body)
		out.Close()

		if err != nil {
			os.Remove(outputPath)
			lastErr = fmt.Errorf("ошибка при записи видео в файл: %v", err)
			continue
		}

		if n < 1024 {
			os.Remove(outputPath)
			lastErr = fmt.Errorf("скачанный файл слишком маленький (%d байт), возможно это не видео", n)
			continue
		}

		return outputPath, nil
	}

	// Если мы здесь, значит все попытки не удались
	return "", fmt.Errorf("не удалось скачать видео после %d попыток: %v", maxRetries, lastErr)
}
