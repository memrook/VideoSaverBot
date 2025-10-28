package main

import (
	"flag"
	"fmt"
	"goland/VideoSaverBot/downloader"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	instagramRegex = regexp.MustCompile(`^https?://(?:www\.)?instagram\.com/(?:p|reel|reels|tv|stories|share)/([^/?#&]+).*`)
	twitterRegex   = regexp.MustCompile(`^https://(?:x|twitter)\.com(?:/(?:i/web|[^/]+)/status/(\d+)(?:.*)?)?$`)
	tiktokRegex    = regexp.MustCompile(`^https?://(?:www\.|m\.|vm\.|vt\.)?tiktok\.com/(?:@[^/]+/(?:video|photo)/\d+|v/\d+|t/[\w]+|[\w]+)/?`)
	facebookRegex  = regexp.MustCompile(`^https?://(?:www\.|web\.|m\.)?facebook\.com/(?:watch\?v=[0-9]+|watch/\?v=[0-9]+|reel/[0-9]+|[a-zA-Z0-9.\-_]+/(?:videos|posts)/[0-9]+|[0-9]+/(?:videos|posts)/[0-9]+|share/(?:v|r)/[a-zA-Z0-9]+)(?:[^/?#&]+.*)?$|^https://fb\.watch/[a-zA-Z0-9]+$`)
	youtubeRegex   = regexp.MustCompile(`^(?:https?://)?(?:www\.)?youtube\.com/shorts/([a-zA-Z0-9_-]{11})(?:\S+)?$`)

	_userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"

	downloadSemaphore chan struct{}
)

func main() {
	botTokenFlag := flag.String("token", "", "Токен Telegram бота")
	debugModeFlag := flag.Bool("debug", false, "Режим отладки (true/false)")
	maxConcurrentDownloads := flag.Int("concurrent", 5, "Максимальное количество одновременных скачиваний")
	flag.Parse()

	if err := checkYtDlpAvailability(); err != nil {
		log.Printf("Предупреждение: yt-dlp недоступен, YouTube функционал будет отключен: %v", err)
	} else {
		log.Println("yt-dlp обнаружен, YouTube функционал включен")
	}

	downloadSemaphore = make(chan struct{}, *maxConcurrentDownloads)

	botToken := *botTokenFlag
	if botToken == "" {
		botToken = os.Getenv("TELEGRAM_BOT_TOKEN")
		if botToken == "" {
			log.Fatal("Токен бота не найден. Установите переменную окружения TELEGRAM_BOT_TOKEN или используйте флаг -token")
		}
	}

	client, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatalf("Ошибка инициализации бота: %v", err)
	}

	client.Debug = *debugModeFlag
	log.Printf("Авторизован как %s (Режим отладки: %v)", client.Self.UserName, client.Debug)

	setupBotCommands(client)

	go startPeriodicCleanup()

	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 30

	updates := client.GetUpdatesChan(updateConfig)

	connectionErrors := make(chan error)
	reconnect := make(chan struct{})

	// Запускаем мониторинг соединения с Telegram API
	go monitorConnection(client, connectionErrors, reconnect)

	for {
		select {
		case update := <-updates:
			if update.Message != nil {
				go handleMessage(client, update.Message)
			}
		case err := <-connectionErrors:
			if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "EOF") {
				log.Printf("Ошибка соединения с Telegram API: %v", err)
			}

			reconnect <- struct{}{}
		case <-reconnect:
			updates = client.GetUpdatesChan(updateConfig)
		}
	}
}

// monitorConnection следит за соединением с Telegram API
func monitorConnection(bot *tgbotapi.BotAPI, errorChan chan<- error, reconnect chan<- struct{}) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		_, err := bot.GetMe()
		if err != nil {
			errorChan <- err
		}
	}
}

func setupBotCommands(bot *tgbotapi.BotAPI) {
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Начать работу с ботом"},
		{Command: "help", Description: "Показать инструкцию по использованию"},
	}

	_, err := bot.Request(tgbotapi.NewSetMyCommands(commands...))
	if err != nil {
		log.Printf("Ошибка при установке команд бота: %v", err)
	}

	scope := tgbotapi.NewBotCommandScopeAllGroupChats()
	_, err = bot.Request(tgbotapi.NewSetMyCommandsWithScope(scope, commands...))
	if err != nil {
		log.Printf("Ошибка при установке команд бота для групповых чатов: %v", err)
	}
}

// acquireSemaphore блокирует семафор для ограничения количества одновременных скачиваний
func acquireSemaphore() {
	downloadSemaphore <- struct{}{}
}

func releaseSemaphore() {
	<-downloadSemaphore
}

func isJustLink(text string, regex *regexp.Regexp) bool {
	trimmedText := strings.TrimSpace(text)

	matches := regex.FindAllString(trimmedText, -1)
	if len(matches) == 0 {
		return false
	}

	return len(trimmedText) == len(matches[0])
}

func extractLink(text string) string {
	instagramMatches := instagramRegex.FindStringSubmatch(text)
	if len(instagramMatches) > 0 {
		return instagramMatches[0]
	}

	twitterMatches := twitterRegex.FindStringSubmatch(text)
	if len(twitterMatches) > 0 {
		return twitterMatches[0]
	}

	tiktokMatches := tiktokRegex.FindStringSubmatch(text)
	if len(tiktokMatches) > 0 {
		return tiktokMatches[0]
	}

	facebookMatches := facebookRegex.FindStringSubmatch(text)
	if len(facebookMatches) > 0 {
		return facebookMatches[0]
	}

	youtubeMatches := youtubeRegex.FindStringSubmatch(text)
	if len(youtubeMatches) > 0 {
		link := youtubeMatches[0]
		if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
			link = "https://" + link
		}
		return link
	}

	return text
}

func handleMessage(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID
	isGroup := message.Chat.IsGroup() || message.Chat.IsSuperGroup()

	if bot.Debug {
		log.Printf("[%s] %s в чате %d (групповой: %v)", message.From.UserName, message.Text, chatID, isGroup)
	}

	if isGroup {
		mentionsBot := false
		if message.Entities != nil {
			for _, entity := range message.Entities {
				if entity.Type == "mention" {
					mention := message.Text[entity.Offset : entity.Offset+entity.Length]
					if strings.Contains(mention, "@"+bot.Self.UserName) {
						mentionsBot = true
						break
					}
				}
			}
		}

		if !message.IsCommand() && !mentionsBot &&
			!isJustLink(message.Text, instagramRegex) &&
			!isJustLink(message.Text, twitterRegex) &&
			!isJustLink(message.Text, tiktokRegex) &&
			!isJustLink(message.Text, facebookRegex) &&
			!isJustLink(message.Text, youtubeRegex) {
			return
		}
	}

	if message.IsCommand() {
		switch message.Command() {
		case "start":
			if isGroup {
				msg := tgbotapi.NewMessage(chatID,
					"Привет! Я готов скачивать видео из Instagram, Twitter, TikTok, Facebook и YouTube Shorts. Просто отправь мне ссылку.")
				bot.Send(msg)
			} else {
				msg := tgbotapi.NewMessage(chatID,
					"Привет! Я бот для скачивания видео из Instagram, Twitter (X), TikTok, Facebook и YouTube Shorts. "+
						"Просто отправь мне ссылку на пост, и я сохраню для тебя видео.\n\n")
				bot.Send(msg)
			}
			return
		case "help":
			helpText := "🔍 *Как использовать*:\n\n" +
				"1. Найдите видео в Instagram, Twitter (X), TikTok, Facebook или YouTube Shorts\n" +
				"2. Скопируйте ссылку на пост/видео\n" +
				"3. Отправьте мне эту ссылку\n" +
				"4. Дождитесь загрузки и получите видео\n\n" +
				"*Поддерживаемые платформы*:\n" +
				"• Instagram (посты и reels)\n" +
				"• Twitter/X\n" +
				"• TikTok\n" +
				"• Facebook\n" +
				"• YouTube Shorts (только короткие видео)\n\n" +
				"*YouTube*: Поддерживаю только Shorts (youtube.com/shorts/). Для длинных видео используйте сторонние сайты.\n\n" +
				"*В групповых чатах*: Я обрабатываю только ссылки на видео или сообщения, в которых меня упоминают (@" + bot.Self.UserName + ")"

			msg := tgbotapi.NewMessage(chatID, helpText)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
			return
		}
	}

	messageText := extractLink(message.Text)

	if instagramRegex.MatchString(messageText) {
		processingMsg, _ := bot.Send(
			tgbotapi.NewMessage(chatID, "Обрабатываю Instagram ссылку..."))

		acquireSemaphore()
		defer releaseSemaphore()

		videoPath, err := downloader.DownloadInstagramVideo(messageText, userID)
		if err != nil {
			errorMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка при скачивании видео: %v", err))
			bot.Send(errorMsg)
			go deleteMessageAfterDelay(bot, chatID, processingMsg.MessageID, 10)
			return
		}

		sendVideo(bot, chatID, videoPath, userID, processingMsg.MessageID)

		go cleanupOldFiles(userID)

	} else if twitterRegex.MatchString(messageText) {
		processingMsg, _ := bot.Send(
			tgbotapi.NewMessage(chatID, "Обрабатываю Twitter/X ссылку..."))

		acquireSemaphore()
		defer releaseSemaphore()

		videoPath, err := downloader.DownloadTwitterVideo(messageText, userID)
		if err != nil {
			errorMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка при скачивании видео: %v", err))
			bot.Send(errorMsg)
			go deleteMessageAfterDelay(bot, chatID, processingMsg.MessageID, 10)
			return
		}

		sendVideo(bot, chatID, videoPath, userID, processingMsg.MessageID)

		go cleanupOldFiles(userID)

	} else if tiktokRegex.MatchString(messageText) {
		processingMsg, _ := bot.Send(
			tgbotapi.NewMessage(chatID, "Обрабатываю TikTok ссылку..."))

		acquireSemaphore()
		defer releaseSemaphore()

		videoPath, err := downloader.DownloadTikTokVideo(messageText, userID)
		if err != nil {
			errorMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка при скачивании видео: %v", err))
			bot.Send(errorMsg)
			go deleteMessageAfterDelay(bot, chatID, processingMsg.MessageID, 10)
			return
		}

		sendVideo(bot, chatID, videoPath, userID, processingMsg.MessageID)

		go cleanupOldFiles(userID)

	} else if facebookRegex.MatchString(messageText) {
		processingMsg, _ := bot.Send(
			tgbotapi.NewMessage(chatID, "Обрабатываю Facebook ссылку..."))

		acquireSemaphore()
		defer releaseSemaphore()

		videoPath, err := downloader.DownloadFacebookVideo(messageText, userID)
		if err != nil {
			errorMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка при скачивании видео: %v", err))
			bot.Send(errorMsg)
			go deleteMessageAfterDelay(bot, chatID, processingMsg.MessageID, 10)
			return
		}

		sendVideo(bot, chatID, videoPath, userID, processingMsg.MessageID)

		go cleanupOldFiles(userID)

	} else if youtubeRegex.MatchString(messageText) {
		processingMsg, _ := bot.Send(
			tgbotapi.NewMessage(chatID, "Обрабатываю YouTube ссылку..."))

		acquireSemaphore()
		defer releaseSemaphore()

		videoPath, err := downloader.DownloadYouTubeVideo(messageText, userID)
		if err != nil {
			errorMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка при скачивании видео: %v", err))
			bot.Send(errorMsg)
			go deleteMessageAfterDelay(bot, chatID, processingMsg.MessageID, 10)
			return
		}

		sendVideo(bot, chatID, videoPath, userID, processingMsg.MessageID)

		go cleanupOldFiles(userID)

	} else if !isGroup {

		normalYouTubeRegex := regexp.MustCompile(`^(?:https?://)?(?:www\.)?(?:youtube\.com/watch\?v=|youtu\.be/)([a-zA-Z0-9_-]{11})`)
		if normalYouTubeRegex.MatchString(messageText) {
			msg := tgbotapi.NewMessage(chatID,
				"Я поддерживаю только YouTube Shorts (короткие видео).\n\n"+
					"Ссылки должны быть вида: youtube.com/shorts/VIDEO_ID\n\n"+
					"Для скачивания обычных YouTube видео используйте сторонние сайты, например:\n"+
					"• savefrom.net\n"+
					"• y2mate.com\n"+
					"• 9xbuddy.com")
			bot.Send(msg)
		} else {
			msg := tgbotapi.NewMessage(chatID,
				"Пожалуйста, отправьте ссылку на пост из Instagram, Twitter, TikTok, Facebook или YouTube Shorts, содержащий видео.")
			bot.Send(msg)
		}
	}
}

func deleteMessageAfterDelay(bot *tgbotapi.BotAPI, chatID int64, messageID int, delaySeconds int) {
	time.Sleep(time.Duration(delaySeconds) * time.Second)
	deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
	if _, err := bot.Request(deleteMsg); err != nil {
		log.Printf("Не удалось удалить сообщение %d: %v", messageID, err)
	}
}

func sendVideo(bot *tgbotapi.BotAPI, chatID int64, videoPath string, userID int64, processingMsgID int) {
	video := tgbotapi.NewVideo(chatID, tgbotapi.FilePath(videoPath))
	//video.Caption = "Вот ваше видео!"

	var err error
	videoSent := false

	defer func() {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, processingMsgID)
		if _, delErr := bot.Request(deleteMsg); delErr != nil {
			log.Printf("Не удалось удалить служебное сообщение %d: %v", processingMsgID, delErr)
		}

		if videoSent {
			if fileErr := os.Remove(videoPath); fileErr != nil {
				log.Printf("Не удалось удалить временный файл %s: %v", videoPath, fileErr)
			}
		}
	}()

	_, err = bot.Send(video)

	if err != nil {
		log.Printf("Ошибка при отправке видео пользователю %d: %v", userID, err)
		errorMsg := tgbotapi.NewMessage(chatID, "Не удалось отправить видео. Попробуйте еще раз.")
		bot.Send(errorMsg)
	} else {
		videoSent = true
	}
}

func cleanupOldFiles(userID int64) {
	userDir := filepath.Join("temp_videos", strconv.FormatInt(userID, 10))

	_, err := os.Stat(userDir)
	if os.IsNotExist(err) {
		return
	}

	files, err := os.ReadDir(userDir)
	if err != nil {
		log.Printf("Ошибка при чтении директории пользователя %d: %v", userID, err)
		return
	}

	now := time.Now()

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filePath := filepath.Join(userDir, file.Name())
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			continue
		}

		if now.Sub(fileInfo.ModTime()) > time.Hour {
			if err := os.Remove(filePath); err != nil {
				log.Printf("Ошибка при удалении старого файла %s: %v", filePath, err)
			} else {
				log.Printf("Удален старый файл: %s", filePath)
			}
		}
	}
}

func startPeriodicCleanup() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	log.Println("Запущена периодическая очистка временных файлов")

	for range ticker.C {
		cleanupAllTempFiles()
	}
}

func cleanupAllTempFiles() {
	log.Println("Начинаем очистку всех временных файлов...")

	tempDir := "temp_videos"

	_, err := os.Stat(tempDir)
	if os.IsNotExist(err) {
		return
	}

	userDirs, err := os.ReadDir(tempDir)
	if err != nil {
		log.Printf("Ошибка при чтении директории временных файлов: %v", err)
		return
	}

	for _, userDir := range userDirs {
		if !userDir.IsDir() {
			continue
		}

		userDirPath := filepath.Join(tempDir, userDir.Name())

		files, err := os.ReadDir(userDirPath)
		if err != nil {
			log.Printf("Ошибка при чтении директории пользователя %s: %v", userDir.Name(), err)
			continue
		}

		now := time.Now()

		hasFiles := false

		for _, file := range files {
			if file.IsDir() {
				continue
			}

			filePath := filepath.Join(userDirPath, file.Name())
			fileInfo, err := os.Stat(filePath)
			if err != nil {
				continue
			}

			if now.Sub(fileInfo.ModTime()) > 24*time.Hour {
				if err := os.Remove(filePath); err != nil {
					log.Printf("Ошибка при удалении старого файла %s: %v", filePath, err)
				} else {
					log.Printf("Удален старый файл: %s", filePath)
				}
			} else {
				hasFiles = true
			}
		}

		if !hasFiles {
			if err := os.Remove(userDirPath); err != nil {
				log.Printf("Ошибка при удалении пустой директории пользователя %s: %v", userDirPath, err)
			} else {
				log.Printf("Удалена пустая директория пользователя: %s", userDirPath)
			}
		}
	}

	log.Println("Очистка временных файлов завершена")
}

func checkYtDlpAvailability() error {
	cmd := exec.Command("yt-dlp", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("yt-dlp не установлен или недоступен: %v", err)
	}
	return nil
}
