package main

import (
	"flag"
	"fmt"
	"goland/VideoSaverBot/downloader"
	"log"
	"os"
	"regexp"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	instagramRegex = regexp.MustCompile(`https?://(www\.)?(instagram\.com|instagr\.am)/(p|reel)/[a-zA-Z0-9_-]+`)
	twitterRegex   = regexp.MustCompile(`https?://(www\.)?(twitter\.com|x\.com)/[a-zA-Z0-9_]+/status/[0-9]+`)
)

func main() {
	// Определение параметров командной строки
	botTokenFlag := flag.String("token", "", "Токен Telegram бота")
	debugModeFlag := flag.Bool("debug", false, "Режим отладки (true/false)")
	flag.Parse()

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
			processingMsg, _ := bot.Send(
				tgbotapi.NewMessage(message.Chat.ID, "Обрабатываю Instagram ссылку..."))

			// Скачивание видео
			videoPath, err := downloader.DownloadInstagramVideo(message.Text)
			if err != nil {
				errorMsg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Ошибка при скачивании видео: %v", err))
				bot.Send(errorMsg)
				// Удаляем сообщение об ошибке через 10 секунд
				go deleteMessageAfterDelay(bot, message.Chat.ID, processingMsg.MessageID, 10)
				continue
			}

			// Отправка видео и удаление сообщения о загрузке
			sendVideo(bot, message.Chat.ID, videoPath, userID, processingMsg.MessageID)

		} else if twitterRegex.MatchString(message.Text) {
			// Отправка сообщения о получении ссылки
			processingMsg, _ := bot.Send(
				tgbotapi.NewMessage(message.Chat.ID, "Обрабатываю Twitter/X ссылку..."))

			// Скачивание видео
			videoPath, err := downloader.DownloadTwitterVideo(message.Text)
			if err != nil {
				errorMsg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Ошибка при скачивании видео: %v", err))
				bot.Send(errorMsg)
				// Удаляем сообщение об ошибке через 10 секунд
				go deleteMessageAfterDelay(bot, message.Chat.ID, processingMsg.MessageID, 10)
				continue
			}

			// Отправка видео и удаление сообщения о загрузке
			sendVideo(bot, message.Chat.ID, videoPath, userID, processingMsg.MessageID)

		} else {
			// Если сообщение не содержит ссылку на Instagram или Twitter
			msg := tgbotapi.NewMessage(message.Chat.ID,
				"Пожалуйста, отправьте ссылку на пост Instagram или Twitter, содержащий видео.")
			bot.Send(msg)
		}
	}
}

// Удаление сообщения после задержки (в секундах)
func deleteMessageAfterDelay(bot *tgbotapi.BotAPI, chatID int64, messageID int, delaySeconds int) {
	time.Sleep(time.Duration(delaySeconds) * time.Second)
	deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
	if _, err := bot.Send(deleteMsg); err != nil {
		log.Printf("Не удалось удалить сообщение %d: %v", messageID, err)
	}
}

// Отправка видео пользователю
func sendVideo(bot *tgbotapi.BotAPI, chatID int64, videoPath string, userID int64, processingMsgID int) {
	// Подготовка файла для отправки
	video := tgbotapi.NewVideo(chatID, tgbotapi.FilePath(videoPath))
	//video.Caption = "Вот ваше видео!"

	// Отправка видео
	_, err := bot.Send(video)

	// Удаляем сообщение "Обрабатываю..." независимо от результата
	deleteMsg := tgbotapi.NewDeleteMessage(chatID, processingMsgID)
	if _, delErr := bot.Send(deleteMsg); delErr != nil {
		log.Printf("Не удалось удалить служебное сообщение %d: %v", processingMsgID, delErr)
	}

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
