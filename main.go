package main

import (
	"fmt"
	"goland/VideoSaverBot/downloader"
	"log"
	"os"
	"regexp"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	instagramRegex = regexp.MustCompile(`https?://(www\.)?(instagram\.com|instagr\.am)/(p|reel)/[a-zA-Z0-9_-]+`)
	twitterRegex   = regexp.MustCompile(`https?://(www\.)?(twitter\.com|x\.com)/[a-zA-Z0-9_]+/status/[0-9]+`)
)

func main() {
	// Получение токена из переменной окружения
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("Токен бота не найден. Установите переменную окружения TELEGRAM_BOT_TOKEN")
	}

	// Инициализация бота
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatalf("Ошибка инициализации бота: %v", err)
	}

	bot.Debug = true // В продакшне установите false
	log.Printf("Авторизован как %s", bot.Self.UserName)

	// Настройка апдейтов
	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60

	updates := bot.GetUpdatesChan(updateConfig)

	// Обработка сообщений
	for update := range updates {
		if update.Message == nil {
			continue
		}

		message := update.Message
		userID := message.From.ID

		log.Printf("[%s] %s", message.From.UserName, message.Text)

		// Отправка приветственного сообщения при команде /start
		if message.IsCommand() && message.Command() == "start" {
			msg := tgbotapi.NewMessage(message.Chat.ID,
				"Привет! Я бот для скачивания видео из Instagram и Twitter (X). "+
					"Просто отправь мне ссылку на пост, и я сохраню для тебя видео. "+
					"Теперь с улучшенной технологией извлечения видео и повышенной стабильностью!")
			bot.Send(msg)
			continue
		}

		// Обработка ссылок
		if instagramRegex.MatchString(message.Text) {
			// Отправка сообщения о получении ссылки
			msg := tgbotapi.NewMessage(message.Chat.ID, "Обрабатываю Instagram ссылку через улучшенный и стабильный метод...")
			bot.Send(msg)

			// Скачивание видео
			videoPath, err := downloader.DownloadInstagramVideo(message.Text)
			if err != nil {
				errorMsg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Ошибка при скачивании видео: %v", err))
				bot.Send(errorMsg)
				continue
			}

			// Отправка видео
			sendVideo(bot, message.Chat.ID, videoPath, userID)

		} else if twitterRegex.MatchString(message.Text) {
			// Отправка сообщения о получении ссылки
			msg := tgbotapi.NewMessage(message.Chat.ID, "Обрабатываю Twitter/X ссылку через быстрый и надежный VX Twitter...")
			bot.Send(msg)

			// Скачивание видео
			videoPath, err := downloader.DownloadTwitterVideo(message.Text)
			if err != nil {
				errorMsg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Ошибка при скачивании видео: %v", err))
				bot.Send(errorMsg)
				continue
			}

			// Отправка видео
			sendVideo(bot, message.Chat.ID, videoPath, userID)

		} else {
			// Если сообщение не содержит ссылку на Instagram или Twitter
			msg := tgbotapi.NewMessage(message.Chat.ID,
				"Пожалуйста, отправьте ссылку на пост Instagram или Twitter, содержащий видео.")
			bot.Send(msg)
		}
	}
}

// Отправка видео пользователю
func sendVideo(bot *tgbotapi.BotAPI, chatID int64, videoPath string, userID int64) {
	// Подготовка файла для отправки
	video := tgbotapi.NewVideo(chatID, tgbotapi.FilePath(videoPath))
	//video.Caption = "Вот ваше видео!"

	// Отправка видео
	_, err := bot.Send(video)
	if err != nil {
		log.Printf("Ошибка при отправке видео пользователю %d: %v", userID, err)
		errorMsg := tgbotapi.NewMessage(chatID, "Не удалось отправить видео. Попробуйте еще раз.")
		bot.Send(errorMsg)
	}

	// Удаление временного файла
	err = os.Remove(videoPath)
	if err != nil {
		log.Printf("Не удалось удалить временный файл %s: %v", videoPath, err)
	}
}
