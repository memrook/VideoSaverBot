package main

import (
	"flag"
	"fmt"
	"goland/VideoSaverBot/downloader"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	instagramRegex = regexp.MustCompile(`https?://(www\.)?(instagram\.com|instagr\.am)/(p|reel)/[a-zA-Z0-9_-]+`)
	twitterRegex   = regexp.MustCompile(`https?://(www\.)?(twitter\.com|x\.com)/[a-zA-Z0-9_]+/status/[0-9]+`)
	// Семафор для ограничения количества одновременных запросов на скачивание
	downloadSemaphore chan struct{}
)

func main() {
	// Определение параметров командной строки
	botTokenFlag := flag.String("token", "", "Токен Telegram бота")
	debugModeFlag := flag.Bool("debug", false, "Режим отладки (true/false)")
	maxConcurrentDownloads := flag.Int("concurrent", 5, "Максимальное количество одновременных скачиваний")
	flag.Parse()

	// Инициализация семафора для ограничения количества одновременных скачиваний
	downloadSemaphore = make(chan struct{}, *maxConcurrentDownloads)

	// Получение токена из параметров или переменной окружения
	botToken := *botTokenFlag
	if botToken == "" {
		botToken = os.Getenv("TELEGRAM_BOT_TOKEN")
		if botToken == "" {
			log.Fatal("Токен бота не найден. Установите переменную окружения TELEGRAM_BOT_TOKEN или используйте флаг -token")
		}
	}

	// Инициализация бота
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatalf("Ошибка инициализации бота: %v", err)
	}

	bot.Debug = *debugModeFlag // Установка режима отладки из параметра
	log.Printf("Авторизован как %s (Режим отладки: %v)", bot.Self.UserName, bot.Debug)

	// Настраиваем команды бота
	setupBotCommands(bot)

	// Запускаем фоновую задачу для периодической очистки старых файлов
	go startPeriodicCleanup()

	// Настройка апдейтов
	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60

	updates := bot.GetUpdatesChan(updateConfig)

	// Обработка сообщений
	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Запускаем обработку каждого сообщения в отдельной горутине для одновременной работы с несколькими пользователями
		go handleMessage(bot, update.Message)
	}
}

// setupBotCommands устанавливает список команд бота для отображения в меню
func setupBotCommands(bot *tgbotapi.BotAPI) {
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Начать работу с ботом"},
		{Command: "help", Description: "Показать инструкцию по использованию"},
	}

	// Устанавливаем команды для обычных чатов
	_, err := bot.Request(tgbotapi.NewSetMyCommands(commands...))
	if err != nil {
		log.Printf("Ошибка при установке команд бота: %v", err)
	}

	// Устанавливаем команды для групповых чатов
	scope := tgbotapi.NewBotCommandScopeAllGroupChats()
	_, err = bot.Request(tgbotapi.NewSetMyCommandsWithScope(scope, commands...))
	if err != nil {
		log.Printf("Ошибка при установке команд бота для групповых чатов: %v", err)
	}
}

// Функция для получения семафора (блокирует, если достигнут лимит)
func acquireSemaphore() {
	downloadSemaphore <- struct{}{}
}

// Функция для освобождения семафора
func releaseSemaphore() {
	<-downloadSemaphore
}

// handleMessage обрабатывает входящее сообщение
func handleMessage(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID
	isGroup := message.Chat.IsGroup() || message.Chat.IsSuperGroup()

	if bot.Debug {
		log.Printf("[%s] %s в чате %d (групповой: %v)", message.From.UserName, message.Text, chatID, isGroup)
	}

	// Для групповых чатов обрабатываем только команды и сообщения, адресованные боту
	if isGroup {
		// Проверяем, упоминается ли бот в сообщении (для групповых чатов)
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

		// В группах обрабатываем только прямые команды или упоминания бота, или чистые ссылки
		if !message.IsCommand() && !mentionsBot &&
			!isJustLink(message.Text, instagramRegex) &&
			!isJustLink(message.Text, twitterRegex) {
			return // Игнорируем сообщения в группах, если они не адресованы боту и не являются чистыми ссылками
		}
	}

	// Обработка команд
	if message.IsCommand() {
		switch message.Command() {
		case "start":
			// В групповых чатах отправляем краткое приветствие
			if isGroup {
				msg := tgbotapi.NewMessage(chatID,
					"Привет! Я готов скачивать видео из Instagram и Twitter. Просто отправь мне ссылку.")
				bot.Send(msg)
			} else {
				// В личных чатах отправляем полное приветствие
				msg := tgbotapi.NewMessage(chatID,
					"Привет! Я бот для скачивания видео из Instagram и Twitter (X). "+
						"Просто отправь мне ссылку на пост, и я сохраню для тебя видео. "+
						"Теперь с улучшенной технологией извлечения видео и повышенной стабильностью!")
				bot.Send(msg)
			}
			return
		case "help":
			helpText := "🔍 *Как использовать*:\n\n" +
				"1. Найдите видео в Instagram или Twitter (X)\n" +
				"2. Скопируйте ссылку на пост\n" +
				"3. Отправьте мне эту ссылку\n" +
				"4. Дождитесь загрузки и получите видео\n\n" +
				"*В групповых чатах*: Я обрабатываю только ссылки на видео или сообщения, в которых меня упоминают (@" + bot.Self.UserName + ")"

			msg := tgbotapi.NewMessage(chatID, helpText)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
			return
		}
	}

	// Обработка ссылок
	if instagramRegex.MatchString(message.Text) {
		// Отправка сообщения о получении ссылки
		processingMsg, _ := bot.Send(
			tgbotapi.NewMessage(chatID, "Обрабатываю Instagram ссылку..."))

		// Получаем семафор (блокирует, если достигнут лимит одновременных скачиваний)
		acquireSemaphore()
		defer releaseSemaphore()

		// Скачивание видео
		videoPath, err := downloader.DownloadInstagramVideo(message.Text, userID)
		if err != nil {
			errorMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка при скачивании видео: %v", err))
			bot.Send(errorMsg)
			// Удаляем сообщение об ошибке через 10 секунд
			go deleteMessageAfterDelay(bot, chatID, processingMsg.MessageID, 10)
			return
		}

		// Отправка видео и удаление сообщения о загрузке
		sendVideo(bot, chatID, videoPath, userID, processingMsg.MessageID)

		// Запускаем очистку старых файлов для этого пользователя
		go cleanupOldFiles(userID)

	} else if twitterRegex.MatchString(message.Text) {
		// Отправка сообщения о получении ссылки
		processingMsg, _ := bot.Send(
			tgbotapi.NewMessage(chatID, "Обрабатываю Twitter/X ссылку..."))

		// Получаем семафор (блокирует, если достигнут лимит одновременных скачиваний)
		acquireSemaphore()
		defer releaseSemaphore()

		// Скачивание видео
		videoPath, err := downloader.DownloadTwitterVideo(message.Text, userID)
		if err != nil {
			errorMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка при скачивании видео: %v", err))
			bot.Send(errorMsg)
			// Удаляем сообщение об ошибке через 10 секунд
			go deleteMessageAfterDelay(bot, chatID, processingMsg.MessageID, 10)
			return
		}

		// Отправка видео и удаление сообщения о загрузке
		sendVideo(bot, chatID, videoPath, userID, processingMsg.MessageID)

		// Запускаем очистку старых файлов для этого пользователя
		go cleanupOldFiles(userID)

	} else if !isGroup {
		// Если сообщение не содержит ссылку на Instagram или Twitter и это личный чат
		// В групповых чатах не отвечаем на сообщения без ссылок
		msg := tgbotapi.NewMessage(chatID,
			"Пожалуйста, отправьте ссылку на пост Instagram или Twitter, содержащий видео.")
		bot.Send(msg)
	}
}

// isJustLink проверяет, является ли сообщение "чистой" ссылкой (содержит только ссылку)
func isJustLink(text string, regex *regexp.Regexp) bool {
	// Очищаем текст от пробелов в начале и конце
	trimmedText := strings.TrimSpace(text)

	// Извлекаем все ссылки из текста
	matches := regex.FindAllString(trimmedText, -1)
	if len(matches) == 0 {
		return false
	}

	// Проверяем, является ли текст только ссылкой (с возможными пробелами в начале/конце)
	return len(trimmedText) == len(matches[0])
}

// Удаление сообщения после задержки (в секундах)
func deleteMessageAfterDelay(bot *tgbotapi.BotAPI, chatID int64, messageID int, delaySeconds int) {
	time.Sleep(time.Duration(delaySeconds) * time.Second)
	deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
	if _, err := bot.Request(deleteMsg); err != nil {
		log.Printf("Не удалось удалить сообщение %d: %v", messageID, err)
	}
}

// Отправка видео пользователю
func sendVideo(bot *tgbotapi.BotAPI, chatID int64, videoPath string, userID int64, processingMsgID int) {
	// Подготовка файла для отправки
	video := tgbotapi.NewVideo(chatID, tgbotapi.FilePath(videoPath))
	//video.Caption = "Вот ваше видео!"

	var err error
	videoSent := false

	// Замыкаем операции с файлом в отложенную функцию для гарантированной обработки
	defer func() {
		// Удаляем сообщение "Обрабатываю..." независимо от результата
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, processingMsgID)
		if _, delErr := bot.Request(deleteMsg); delErr != nil {
			log.Printf("Не удалось удалить служебное сообщение %d: %v", processingMsgID, delErr)
		}

		// Удаление временного файла только если он существует и видео успешно отправлено
		if videoSent {
			if fileErr := os.Remove(videoPath); fileErr != nil {
				log.Printf("Не удалось удалить временный файл %s: %v", videoPath, fileErr)
			}
		}
	}()

	// Отправка видео
	_, err = bot.Send(video)

	if err != nil {
		log.Printf("Ошибка при отправке видео пользователю %d: %v", userID, err)
		errorMsg := tgbotapi.NewMessage(chatID, "Не удалось отправить видео. Попробуйте еще раз.")
		bot.Send(errorMsg)
	} else {
		videoSent = true
	}
}

// cleanupOldFiles удаляет старые временные файлы пользователя (старше 1 часа)
func cleanupOldFiles(userID int64) {
	userDir := filepath.Join("temp_videos", strconv.FormatInt(userID, 10))

	// Проверяем существование директории
	_, err := os.Stat(userDir)
	if os.IsNotExist(err) {
		return // Директория не существует, нечего очищать
	}

	// Получаем список файлов в директории пользователя
	files, err := os.ReadDir(userDir)
	if err != nil {
		log.Printf("Ошибка при чтении директории пользователя %d: %v", userID, err)
		return
	}

	// Текущее время
	now := time.Now()

	// Проходим по всем файлам и удаляем те, которые старше 1 часа
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filePath := filepath.Join(userDir, file.Name())
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			continue
		}

		// Если файл старше 1 часа, удаляем его
		if now.Sub(fileInfo.ModTime()) > time.Hour {
			if err := os.Remove(filePath); err != nil {
				log.Printf("Ошибка при удалении старого файла %s: %v", filePath, err)
			} else {
				log.Printf("Удален старый файл: %s", filePath)
			}
		}
	}
}

// startPeriodicCleanup запускает периодическую очистку всех временных файлов
func startPeriodicCleanup() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	log.Println("Запущена периодическая очистка временных файлов")

	for range ticker.C {
		cleanupAllTempFiles()
	}
}

// cleanupAllTempFiles очищает все старые временные файлы (старше 24 часов)
func cleanupAllTempFiles() {
	log.Println("Начинаем очистку всех временных файлов...")

	tempDir := "temp_videos"

	// Проверяем существование директории
	_, err := os.Stat(tempDir)
	if os.IsNotExist(err) {
		return // Директория не существует, нечего очищать
	}

	// Получаем список поддиректорий пользователей
	userDirs, err := os.ReadDir(tempDir)
	if err != nil {
		log.Printf("Ошибка при чтении директории временных файлов: %v", err)
		return
	}

	// Проходим по всем поддиректориям пользователей
	for _, userDir := range userDirs {
		if !userDir.IsDir() {
			continue
		}

		userDirPath := filepath.Join(tempDir, userDir.Name())

		// Получаем список файлов в директории пользователя
		files, err := os.ReadDir(userDirPath)
		if err != nil {
			log.Printf("Ошибка при чтении директории пользователя %s: %v", userDir.Name(), err)
			continue
		}

		// Текущее время
		now := time.Now()

		// Флаг для проверки, есть ли еще файлы в директории
		hasFiles := false

		// Проходим по всем файлам и удаляем те, которые старше 24 часов
		for _, file := range files {
			if file.IsDir() {
				continue
			}

			filePath := filepath.Join(userDirPath, file.Name())
			fileInfo, err := os.Stat(filePath)
			if err != nil {
				continue
			}

			// Если файл старше 24 часов, удаляем его
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

		// Если в директории не осталось файлов, удаляем её
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
