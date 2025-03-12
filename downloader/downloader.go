package downloader

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// DownloadInstagramVideo скачивает видео из Instagram по ссылке на пост
func DownloadInstagramVideo(url string) (string, error) {
	// Создаем директорию для временных файлов, если она не существует
	tempDir := "temp_videos"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("не удалось создать директорию для временных файлов: %v", err)
	}

	// Генерация уникального имени файла
	timestamp := time.Now().UnixNano()
	outputPath := filepath.Join(tempDir, fmt.Sprintf("instagram_%d.mp4", timestamp))

	// Заменяем instagram.com на ddinstagram.com для легкого извлечения видео
	ddUrl := strings.Replace(url, "instagram.com", "ddinstagram.com", 1)

	// Настройка HTTP-клиента с увеличенным таймаутом
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Позволяем до 10 редиректов
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

	// Парсинг HTML-страницы
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка при парсинге HTML: %v", err)
	}

	// Поиск URL видео в метаданных страницы
	var videoURL string

	// Проверяем первым делом meta og:video
	ogVideo, exists := doc.Find("meta[property='og:video']").Attr("content")
	if exists && ogVideo != "" {
		videoURL = ogVideo
	}

	// Если не найдено, проверяем og:video:secure_url
	if videoURL == "" {
		secureVideo, exists := doc.Find("meta[property='og:video:secure_url']").Attr("content")
		if exists && secureVideo != "" {
			videoURL = secureVideo
		}
	}

	// Если через meta-теги не нашли, ищем прямые ссылки на видео в теге video
	if videoURL == "" {
		doc.Find("video source").Each(func(i int, s *goquery.Selection) {
			src, exists := s.Attr("src")
			if exists && strings.Contains(src, ".mp4") && videoURL == "" {
				videoURL = src
			}
		})
	}

	// Также пробуем найти на странице ddinstagram кнопку загрузки HD видео
	if videoURL == "" {
		doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if exists && strings.Contains(href, ".mp4") && videoURL == "" {
				// Если ссылка относительная, добавляем домен
				if !strings.HasPrefix(href, "http") {
					href = "https://ddinstagram.com" + href
				}
				videoURL = href
			}
		})
	}

	// Если до сих пор URL не найден, пробуем второй популярный сервис - instafinsta
	if videoURL == "" {
		instafinstaUrl := strings.Replace(url, "instagram.com", "instafinsta.com/instagram-reels-video-downloader", 1)

		req, err := http.NewRequest("GET", instafinstaUrl, nil)
		if err != nil {
			return "", fmt.Errorf("ошибка при создании запроса к instafinsta: %v", err)
		}

		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("ошибка при запросе к instafinsta: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			doc, err := goquery.NewDocumentFromReader(resp.Body)
			if err == nil {
				doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
					href, exists := s.Attr("href")
					text := s.Text()
					if exists && strings.Contains(href, ".mp4") && strings.Contains(text, "Download") && videoURL == "" {
						videoURL = href
					}
				})
			}
		}
	}

	if videoURL == "" {
		return "", errors.New("не удалось найти URL видео в посте Instagram")
	}

	// Скачивание видео
	return downloadMedia(videoURL, outputPath)
}

// DownloadTwitterVideo скачивает видео из Twitter по ссылке на пост
func DownloadTwitterVideo(url string) (string, error) {
	// Создаем директорию для временных файлов, если она не существует
	tempDir := "temp_videos"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("не удалось создать директорию для временных файлов: %v", err)
	}

	// Генерация уникального имени файла
	timestamp := time.Now().UnixNano()
	outputPath := filepath.Join(tempDir, fmt.Sprintf("twitter_%d.mp4", timestamp))

	// Заменяем x.com на twitter.com, а затем twitter.com на vxtwitter.com
	url = strings.Replace(url, "x.com", "twitter.com", 1)
	vxUrl := strings.Replace(url, "twitter.com", "vxtwitter.com", 1)

	// Настройка HTTP-клиента с увеличенным таймаутом
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Позволяем до 10 редиректов
			if len(via) >= 10 {
				return fmt.Errorf("слишком много редиректов")
			}
			return nil
		},
	}

	// Отправка запроса к vxTwitter для получения HTML страницы
	req, err := http.NewRequest("GET", vxUrl, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка при создании запроса: %v", err)
	}

	// Устанавливаем заголовки для имитации TelegramBot
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

	// Парсинг HTML-страницы
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка при парсинге HTML: %v", err)
	}

	// Поиск URL видео в метаданных страницы
	var videoURL string

	// Проверяем мета-теги с видео (как в TypeScript примере)
	// Ищем twitter:video или og:video
	twitterVideo, exists := doc.Find("meta[name='twitter:video']").Attr("content")
	if exists && twitterVideo != "" {
		videoURL = twitterVideo
	} else {
		ogVideo, exists := doc.Find("meta[property='og:video']").Attr("content")
		if exists && ogVideo != "" {
			videoURL = ogVideo
		}
	}

	// Если не найдено в основных мета-тегах, проверяем дополнительные
	if videoURL == "" {
		twitterPlayerStream, exists := doc.Find("meta[name='twitter:player:stream']").Attr("content")
		if exists && twitterPlayerStream != "" {
			videoURL = twitterPlayerStream
		}
	}

	// Если до сих пор URL не найден, возвращаем ошибку
	if videoURL == "" {
		return "", errors.New("не удалось найти URL видео в посте Twitter")
	}

	// Скачивание видео
	return downloadMedia(videoURL, outputPath)
}

// extractJSONBlocks извлекает блоки JSON из HTML строки
func extractJSONBlocks(htmlContent string) []string {
	var jsonBlocks []string

	// Регулярное выражение для поиска JSON блоков
	re := regexp.MustCompile(`\{(?:[^{}]|(?:\{(?:[^{}]|(?:\{[^{}]*\}))*\}))*\}`)
	matches := re.FindAllString(htmlContent, -1)

	for _, match := range matches {
		// Проверяем, что это похоже на валидный JSON
		if strings.Contains(match, "\"") {
			jsonBlocks = append(jsonBlocks, match)
		}
	}

	return jsonBlocks
}

// findVideoURLInJSON рекурсивно ищет URL видео в JSON объекте
func findVideoURLInJSON(data interface{}) string {
	switch v := data.(type) {
	case map[string]interface{}:
		// Проверяем известные ключи, где может быть URL видео
		for key, value := range v {
			if key == "video_url" || key == "playbackUrl" || key == "url" {
				if url, ok := value.(string); ok {
					if strings.Contains(url, "video.twimg.com") || strings.Contains(url, ".mp4") {
						return url
					}
				}
			}

			// Рекурсивный поиск в дочерних объектах
			if url := findVideoURLInJSON(value); url != "" {
				return url
			}
		}
	case []interface{}:
		// Ищем в каждом элементе массива
		for _, item := range v {
			if url := findVideoURLInJSON(item); url != "" {
				return url
			}
		}
	}

	return ""
}

// downloadMedia скачивает медиа по URL и сохраняет его в outputPath
func downloadMedia(url, outputPath string) (string, error) {
	// Удаляем лишние кавычки и экранированные символы в URL
	url = strings.Trim(url, "\"'")
	url = strings.ReplaceAll(url, "\\", "")

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
