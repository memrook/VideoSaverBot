package downloader

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
)

// Мьютекс для синхронизации создания директорий
var tempDirMutex = &sync.Mutex{}

// SnapsaveResponse представляет ответ от API snapsave.app
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

// PlatformType определяет тип платформы
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

	tNum, err := strconv.Atoi(t)
	if err != nil {
		return ""
	}

	eNum, err := strconv.Atoi(e)
	if err != nil {
		return ""
	}

	// Функция decode из TypeScript
	decode := func(d string, e, f int) string {
		chars := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ+/"
		g := strings.Split(chars, "")

		var hArr, iArr []string
		if e <= len(g) {
			hArr = g[:e]
		} else {
			hArr = g
		}
		if f <= len(g) {
			iArr = g[:f]
		} else {
			iArr = g
		}

		// Обратный порядок и вычисление j
		dRunes := []rune(d)
		j := 0
		for c := 0; c < len(dRunes); c++ {
			b := string(dRunes[len(dRunes)-1-c])
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

		// Построение результата
		k := ""
		for j > 0 {
			k = iArr[j%f] + k
			j = j / f
		}
		if k == "" {
			return "0"
		}
		return k
	}

	result := ""
	nRunes := []rune(n)
	hRunes := []rune(h)

	if eNum >= len(nRunes) {
		return ""
	}

	delimiter := nRunes[eNum]

	for i := 0; i < len(hRunes); {
		s := ""
		// Читаем до разделителя
		for i < len(hRunes) && hRunes[i] != delimiter {
			s += string(hRunes[i])
			i++
		}
		i++ // пропускаем разделитель

		// Заменяем символы из n на их индексы
		for j, char := range nRunes {
			s = strings.ReplaceAll(s, string(char), strconv.Itoa(j))
		}

		// Декодируем и добавляем символ
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

// fixEncoding исправляет кодировку UTF-8
func fixEncoding(str string) string {
	if utf8.ValidString(str) {
		return str
	}

	// Пытаемся исправить неправильную кодировку
	bytes := make([]byte, 0, len(str))
	for _, char := range str {
		if char <= 255 {
			bytes = append(bytes, byte(char))
		}
	}

	if utf8.Valid(bytes) {
		return string(bytes)
	}

	return str
}

// getEncodedSnapApp извлекает закодированные данные из HTML
func getEncodedSnapApp(data string) []string {
	// Ищем паттерн: decodeURIComponent(escape(r))}(
	startPattern := "decodeURIComponent(escape(r))}("
	endPattern := "))"

	startIdx := strings.Index(data, startPattern)
	if startIdx == -1 {
		return nil
	}

	startIdx += len(startPattern)
	endIdx := strings.Index(data[startIdx:], endPattern)
	if endIdx == -1 {
		return nil
	}

	encoded := data[startIdx : startIdx+endIdx]

	// Разделяем по запятым и очищаем от кавычек
	parts := strings.Split(encoded, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		cleaned := strings.Trim(strings.TrimSpace(part), "\"'")
		result = append(result, cleaned)
	}

	return result
}

// getDecodedSnapSave извлекает декодированные данные SnapSave
func getDecodedSnapSave(data string) string {
	startPattern := "getElementById(\"download-section\").innerHTML = \""
	endPattern := "\"; document.getElementById(\"inputData\").remove(); "

	startIdx := strings.Index(data, startPattern)
	if startIdx == -1 {
		return ""
	}

	startIdx += len(startPattern)
	endIdx := strings.Index(data[startIdx:], endPattern)
	if endIdx == -1 {
		return ""
	}

	result := data[startIdx : startIdx+endIdx]
	// Убираем экранирование
	result = strings.ReplaceAll(result, "\\\\", "\\")
	result = strings.ReplaceAll(result, "\\", "")

	return result
}

// decryptSnapSave расшифровывает данные SnapSave
func decryptSnapSave(data string) string {
	encoded := getEncodedSnapApp(data)
	if encoded == nil {
		return ""
	}
	decoded := decodeSnapApp(encoded)
	return getDecodedSnapSave(decoded)
}

// getDecodedSnaptik извлекает декодированные данные Snaptik
func getDecodedSnaptik(data string) string {
	startPattern := "$(\"#download\").innerHTML = \""
	endPattern := "\"; document.getElementById(\"inputData\").remove(); "

	startIdx := strings.Index(data, startPattern)
	if startIdx == -1 {
		return ""
	}

	startIdx += len(startPattern)
	endIdx := strings.Index(data[startIdx:], endPattern)
	if endIdx == -1 {
		return ""
	}

	result := data[startIdx : startIdx+endIdx]
	// Убираем экранирование
	result = strings.ReplaceAll(result, "\\\\", "\\")
	result = strings.ReplaceAll(result, "\\", "")

	return result
}

// decryptSnaptik расшифровывает данные Snaptik
func decryptSnaptik(data string) string {
	encoded := getEncodedSnapApp(data)
	if encoded == nil {
		return ""
	}
	decoded := decodeSnapApp(encoded)
	return getDecodedSnaptik(decoded)
}

// normalizeURL нормализует URL согласно логике snapsave
func normalizeURL(url string) string {
	// Для Twitter URL не изменяем
	twitterRegex := regexp.MustCompile(`^https://(?:x|twitter)\.com(?:/(?:i/web|[^/]+)/status/(\d+)(?:.*)?)?$`)
	if twitterRegex.MatchString(url) {
		return url
	}

	// Для других URL добавляем www, если его нет
	re := regexp.MustCompile(`^(https?://)(?!www\.)[a-z0-9]+`)
	if re.MatchString(url) {
		return regexp.MustCompile(`^(https?://)([^./]+\.[^./]+)(\/.*)?$`).ReplaceAllString(url, "$1www.$2$3")
	}

	return url
}

// fixThumbnail исправляет URL thumbnail
func fixThumbnail(url string) string {
	toReplace := "https://snapinsta.app/photo.php?photo="
	if strings.Contains(url, toReplace) {
		decoded, err := url.QueryUnescape(strings.Replace(url, toReplace, "", 1))
		if err != nil {
			return url
		}
		return decoded
	}
	return url
}

// getUserAgent возвращает стандартный User Agent
func getUserAgent() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
}

// Генерирует уникальный идентификатор файла
func generateUniqueID() string {
	// Генерируем 8 байт случайных данных
	randomBytes := make([]byte, 8)
	_, err := rand.Read(randomBytes)
	if err != nil {
		// Если не удалось сгенерировать случайное число, используем время + 4 доп. байта
		return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix()%10000)
	}

	// Возвращаем hex-представление случайных байтов
	return hex.EncodeToString(randomBytes)
}

// detectPlatform определяет тип платформы по URL
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
		return Instagram // По умолчанию считаем Instagram
	}
}

// snapsaveDownload скачивает медиа через API snapsave.app
func snapsaveDownload(mediaURL string, userID int64) (string, error) {
	platform := detectPlatform(mediaURL)

	// Создаем уникальную поддиректорию для пользователя
	outputPath, err := createUserDirectory(userID, string(platform))
	if err != nil {
		return "", err
	}

	// Пытаемся скачать через snapsave API
	videoURL, err := getSnapsaveVideoURL(mediaURL)
	if err != nil {
		// Если snapsave не сработал, используем fallback методы
		return fallbackDownload(mediaURL, userID, platform)
	}

	// Скачиваем видео по полученному URL
	return downloadMedia(videoURL, outputPath)
}

// getSnapsaveVideoURL получает URL видео через API snapsave.app (упрощенная версия)
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

// getSnapsaveVideoURLTikTok получает URL видео из TikTok через snaptik.app
func getSnapsaveVideoURLTikTok(mediaURL string) (string, error) {
	// Создаем HTTP-клиент
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Шаг 1: Получаем главную страницу snaptik.app для извлечения токена
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

	// Парсим HTML для получения токена
	homeDoc, err := goquery.NewDocumentFromReader(homeResp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга HTML snaptik.app: %v", err)
	}

	token, exists := homeDoc.Find("input[name='token']").Attr("value")
	if !exists || token == "" {
		return "", fmt.Errorf("токен не найден на странице snaptik.app")
	}

	// Шаг 2: Отправляем POST-запрос с URL и токеном
	formData := url.Values{}
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

	// Читаем зашифрованный ответ
	body, err := io.ReadAll(postResp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа от snaptik.app: %v", err)
	}

	// Расшифровываем данные Snaptik (используем ту же функцию декодирования)
	decryptedHTML := decryptSnaptik(string(body))
	if decryptedHTML == "" {
		return "", fmt.Errorf("не удалось расшифровать данные snaptik")
	}

	// Парсим расшифрованный HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(decryptedHTML))
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга расшифрованного HTML snaptik: %v", err)
	}

	// Ищем ссылку на видео
	videoURL, exists := doc.Find(".download-box > .video-links > a").Attr("href")
	if !exists || videoURL == "" {
		return "", fmt.Errorf("видео URL не найден в ответе snaptik")
	}

	return videoURL, nil
}

// getSnapsaveVideoURLTwitter получает URL видео из Twitter через twitterdownloader.snapsave.app
func getSnapsaveVideoURLTwitter(mediaURL string) (string, error) {
	// Создаем HTTP-клиент
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Шаг 1: Получаем главную страницу twitterdownloader.snapsave.app для извлечения токена
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

	// Парсим HTML для получения токена
	homeDoc, err := goquery.NewDocumentFromReader(homeResp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга HTML twitterdownloader.snapsave.app: %v", err)
	}

	token, exists := homeDoc.Find("input[name='token']").Attr("value")
	if !exists || token == "" {
		return "", fmt.Errorf("токен не найден на странице twitterdownloader.snapsave.app")
	}

	// Шаг 2: Отправляем POST-запрос с URL и токеном
	formData := url.Values{}
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

	// Читаем JSON ответ
	body, err := io.ReadAll(postResp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа от twitterdownloader.snapsave.app: %v", err)
	}

	// Парсим JSON для получения HTML данных
	var jsonResponse struct {
		Data string `json:"data"`
	}

	if err := json.Unmarshal(body, &jsonResponse); err != nil {
		return "", fmt.Errorf("ошибка парсинга JSON ответа: %v", err)
	}

	if jsonResponse.Data == "" {
		return "", fmt.Errorf("пустые данные в JSON ответе")
	}

	// Парсим HTML из JSON
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(jsonResponse.Data))
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга HTML из JSON: %v", err)
	}

	// Ищем ссылку на видео
	videoURL, exists := doc.Find("#download-block > .abuttons > a").Attr("href")
	if !exists || videoURL == "" {
		return "", fmt.Errorf("видео URL не найден в ответе twitterdownloader")
	}

	return videoURL, nil
}

// getSnapsaveVideoURLInstagramFacebook получает URL видео из Instagram/Facebook через snapsave.app
func getSnapsaveVideoURLInstagramFacebook(mediaURL string) (string, error) {
	apiURL := "https://snapsave.app/action.php?lang=en"

	// Подготавливаем данные для POST-запроса
	formData := url.Values{}
	formData.Set("url", normalizeURL(mediaURL))

	// Создаем HTTP-клиент
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Создаем POST-запрос
	req, err := http.NewRequest("POST", apiURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %v", err)
	}

	// Устанавливаем заголовки
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", getUserAgent())
	req.Header.Set("Referer", "https://snapsave.app/")
	req.Header.Set("Origin", "https://snapsave.app")

	// Отправляем запрос
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения запроса: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("неверный статус код: %d", resp.StatusCode)
	}

	// Читаем HTML ответ
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	// Расшифровываем данные SnapSave
	decryptedHTML := decryptSnapSave(string(body))
	if decryptedHTML == "" {
		// Если расшифровка не удалась, пробуем fallback через регулярные выражения
		return findVideoURLWithRegex(string(body))
	}

	// Парсим расшифрованный HTML с помощью goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(decryptedHTML))
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга расшифрованного HTML: %v", err)
	}

	// Ищем видео URL в расшифрованном HTML согласно структуре snapsave
	var videoURL string

	// Проверяем таблицу с разными разрешениями
	if doc.Find("table.table").Length() > 0 {
		doc.Find("tbody > tr").Each(func(i int, s *goquery.Selection) {
			td := s.Find("td")
			if td.Length() >= 3 {
				// Ищем ссылку в третьей колонке (индекс 2)
				href, exists := td.Eq(2).Find("a").Attr("href")
				if exists && href != "" && videoURL == "" {
					videoURL = href
				} else {
					// Ищем в onclick кнопки
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

	// Проверяем карточки (div.card)
	if videoURL == "" && doc.Find("div.card").Length() > 0 {
		doc.Find("div.card").Each(func(i int, s *goquery.Selection) {
			cardBody := s.Find("div.card-body")
			href, exists := cardBody.Find("a").Attr("href")
			if exists && href != "" && videoURL == "" {
				videoURL = href
			}
		})
	}

	// Проверяем download-items
	if videoURL == "" && doc.Find("div.download-items").Length() > 0 {
		doc.Find("div.download-items").Each(func(i int, s *goquery.Selection) {
			itemBtn := s.Find("div.download-items__btn")
			href, exists := itemBtn.Find("a").Attr("href")
			if exists && href != "" && videoURL == "" {
				videoURL = href
			}
		})
	}

	// Общий поиск ссылок, если специфические структуры не найдены
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

// findVideoURLWithRegex ищет видео URL через регулярные выражения (fallback метод)
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
			// Если это прогрессивная ссылка, добавляем домен
			if strings.Contains(pattern, "get_progressApi") {
				videoURL = "https://snapsave.app" + videoURL
			}
			return videoURL, nil
		}
	}

	return "", fmt.Errorf("не удалось найти видео URL через регулярные выражения")
}

// createUserDirectory создает уникальную директорию для пользователя
func createUserDirectory(userID int64, platform string) (string, error) {
	tempDirBase := "temp_videos"

	// Используем мьютекс для синхронизации создания директорий
	tempDirMutex.Lock()
	defer tempDirMutex.Unlock()

	if err := os.MkdirAll(tempDirBase, 0755); err != nil {
		return "", fmt.Errorf("не удалось создать базовую директорию для временных файлов: %v", err)
	}

	// Создаем уникальную поддиректорию для пользователя
	userDir := filepath.Join(tempDirBase, strconv.FormatInt(userID, 10))
	if err := os.MkdirAll(userDir, 0755); err != nil {
		return "", fmt.Errorf("не удалось создать директорию пользователя для временных файлов: %v", err)
	}

	// Генерация гарантированно уникального имени файла
	uniqueID := generateUniqueID()
	timestamp := time.Now().UnixNano()
	outputPath := filepath.Join(userDir, fmt.Sprintf("%s_%d_%s_%d.mp4", platform, userID, uniqueID, timestamp))

	return outputPath, nil
}

// fallbackDownload использует старые методы скачивания как резервные
func fallbackDownload(mediaURL string, userID int64, platform PlatformType) (string, error) {
	switch platform {
	case Instagram:
		return fallbackInstagramDownload(mediaURL, userID)
	case Twitter:
		return fallbackTwitterDownload(mediaURL, userID)
	default:
		return "", fmt.Errorf("платформа %s не поддерживается в fallback режиме", platform)
	}
}

// DownloadInstagramVideo скачивает видео из Instagram по ссылке на пост
func DownloadInstagramVideo(url string, userID int64) (string, error) {
	return snapsaveDownload(url, userID)
}

// DownloadTwitterVideo скачивает видео из Twitter по ссылке на пост
func DownloadTwitterVideo(url string, userID int64) (string, error) {
	return snapsaveDownload(url, userID)
}

// DownloadTikTokVideo скачивает видео из TikTok по ссылке на пост
func DownloadTikTokVideo(url string, userID int64) (string, error) {
	return snapsaveDownload(url, userID)
}

// DownloadFacebookVideo скачивает видео из Facebook по ссылке на пост
func DownloadFacebookVideo(url string, userID int64) (string, error) {
	return snapsaveDownload(url, userID)
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

// downloadMedia скачивает медиа по URL и сохраняет его в outputPath
func downloadMedia(url, outputPath string) (string, error) {
	// Удаляем лишние кавычки и экранированные символы в URL
	url = strings.Trim(url, "\"'")
	url = strings.ReplaceAll(url, "\\", "")

	// Проверяем и исправляем относительные URL
	if strings.HasPrefix(url, "/") {
		url = "https://ddinstagram.com" + url
	}

	// Проверяем, что URL начинается с http или https
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("неверный URL формат: %s", url)
	}

	// Настройка HTTP-клиента с повторными попытками
	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Позволяем до 10 редиректов
			if len(via) >= 10 {
				return fmt.Errorf("слишком много редиректов")
			}
			return nil
		},
	}

	// Максимальное количество попыток скачивания
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Отправка запроса для скачивания видео
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			lastErr = fmt.Errorf("ошибка при создании запроса для скачивания: %v", err)
			continue
		}

		// Устанавливаем заголовки для имитации браузера
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
		req.Header.Set("Accept", "video/mp4,video/webm,video/*;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Referer", "https://www.instagram.com/")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("ошибка при скачивании видео (попытка %d): %v", attempt+1, err)
			// Ждем перед повторной попыткой
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("получен неверный статус код при скачивании (попытка %d): %d", attempt+1, resp.StatusCode)
			// Ждем перед повторной попыткой
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}

		// Проверяем тип содержимого
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "video/") && !strings.Contains(contentType, "application/octet-stream") && !strings.Contains(contentType, "binary/") {
			// Если это не видео, проверим размер
			contentLength := resp.ContentLength
			if contentLength > 0 && contentLength < 10000 { // Если размер меньше 10 KB, это вероятно не видео
				lastErr = fmt.Errorf("контент не похож на видео: тип %s, размер %d байт", contentType, contentLength)
				continue
			}
		}

		// Создание файла для сохранения видео
		out, err := os.Create(outputPath)
		if err != nil {
			return "", fmt.Errorf("ошибка при создании файла: %v", err)
		}

		// Копирование данных из ответа в файл с индикатором прогресса
		n, err := io.Copy(out, resp.Body)
		out.Close() // Закрываем файл независимо от результата

		if err != nil {
			// Удаляем неполный файл в случае ошибки
			os.Remove(outputPath)
			lastErr = fmt.Errorf("ошибка при записи видео в файл: %v", err)
			continue
		}

		// Проверяем размер скачанного файла
		if n < 1024 { // Если меньше 1 KB, то это вероятно не видео
			os.Remove(outputPath)
			lastErr = fmt.Errorf("скачанный файл слишком маленький (%d байт), возможно это не видео", n)
			continue
		}

		// Успешно скачали файл
		return outputPath, nil
	}

	// Если мы здесь, значит все попытки не удались
	return "", fmt.Errorf("не удалось скачать видео после %d попыток: %v", maxRetries, lastErr)
}
