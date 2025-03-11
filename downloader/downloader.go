package downloader

import (
	"encoding/json"
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

	// Настройка HTTP-клиента
	client := &http.Client{}

	// Отправка запроса к Instagram для получения HTML страницы
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка при создании запроса: %v", err)
	}

	// Устанавливаем заголовки для имитации браузера
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка при запросе к Instagram: %v", err)
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

	// Проверяем мета-теги с видео
	doc.Find("meta[property='og:video']").Each(func(i int, s *goquery.Selection) {
		if content, exists := s.Attr("content"); exists && videoURL == "" {
			videoURL = content
		}
	})

	// Проверяем скрипты на наличие JSON с данными о видео
	if videoURL == "" {
		doc.Find("script").Each(func(i int, s *goquery.Selection) {
			if strings.Contains(s.Text(), "video_url") {
				jsonStr := s.Text()

				// Извлекаем JSON из скрипта
				re := regexp.MustCompile(`({.*})`)
				matches := re.FindStringSubmatch(jsonStr)

				if len(matches) > 0 {
					var data map[string]interface{}
					if err := json.Unmarshal([]byte(matches[1]), &data); err == nil {
						if graphql, ok := data["graphql"].(map[string]interface{}); ok {
							if shortcode, ok := graphql["shortcode_media"].(map[string]interface{}); ok {
								if videoUrl, ok := shortcode["video_url"].(string); ok {
									videoURL = videoUrl
								}
							}
						}
					}
				}
			}
		})
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

	// Настройка HTTP-клиента
	client := &http.Client{}

	// Отправка запроса к Twitter для получения HTML страницы
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка при создании запроса: %v", err)
	}

	// Устанавливаем заголовки для имитации браузера
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка при запросе к Twitter: %v", err)
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

	// Проверяем мета-теги с видео
	doc.Find("meta[property='og:video:url']").Each(func(i int, s *goquery.Selection) {
		if content, exists := s.Attr("content"); exists && videoURL == "" {
			videoURL = content
		}
	})

	// Проверяем также тег og:video
	if videoURL == "" {
		doc.Find("meta[property='og:video']").Each(func(i int, s *goquery.Selection) {
			if content, exists := s.Attr("content"); exists && videoURL == "" {
				videoURL = content
			}
		})
	}

	// Ищем в скриптах
	if videoURL == "" {
		doc.Find("script").Each(func(i int, s *goquery.Selection) {
			if strings.Contains(s.Text(), "video_url") || strings.Contains(s.Text(), "playbackUrl") {
				jsonStr := s.Text()

				// Извлекаем URL видео с помощью регулярных выражений
				re := regexp.MustCompile(`(?:video_url|playbackUrl)["']?\s*[:=]\s*["']([^"']+)["']`)
				matches := re.FindStringSubmatch(jsonStr)

				if len(matches) > 1 {
					videoURL = matches[1]
				}
			}
		})
	}

	if videoURL == "" {
		return "", errors.New("не удалось найти URL видео в посте Twitter")
	}

	// Скачивание видео
	return downloadMedia(videoURL, outputPath)
}

// downloadMedia скачивает медиа по URL и сохраняет его в outputPath
func downloadMedia(url, outputPath string) (string, error) {
	// Настройка HTTP-клиента
	client := &http.Client{}

	// Отправка запроса для скачивания видео
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка при создании запроса для скачивания: %v", err)
	}

	// Устанавливаем заголовки для имитации браузера
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка при скачивании видео: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("получен неверный статус код при скачивании: %d", resp.StatusCode)
	}

	// Создание файла для сохранения видео
	out, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("ошибка при создании файла: %v", err)
	}
	defer out.Close()

	// Копирование данных из ответа в файл
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка при записи видео в файл: %v", err)
	}

	return outputPath, nil
}
