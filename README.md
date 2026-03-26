# VideoSaverBot для Telegram

Телеграм-бот на Go для скачивания видео из популярных социальных сетей по ссылке.

## Возможности

- Скачивание видео из Instagram, Twitter/X, TikTok, Facebook и YouTube Shorts
- Основной метод: snapsave.app / snaptik.app с автоматической расшифровкой обфусцированных ответов
- Резервные методы при недоступности основного API (DDInstagram, VXTwitter, tikmate.online)
- Корректное соотношение сторон видео — ffprobe определяет размеры перед отправкой в Telegram
- Ограничение параллельных загрузок с обратной связью о позиции в очереди
- Один активный запрос на пользователя одновременно
- Graceful shutdown — SIGTERM ожидает завершения активных загрузок (до 30 с)
- Таймаут 3 минуты на всю цепочку скачивания
- Команда `/stats` для администратора
- Работа в личных и групповых чатах
- Автоматическая очистка временных файлов

## Поддерживаемые платформы

| Платформа | Основной метод | Резервный метод |
|-----------|---------------|-----------------|
| Instagram | snapsave.app | DDInstagram |
| Twitter/X | twitterdownloader.snapsave.app | VXTwitter |
| TikTok | snaptik.app | tikmate.online |
| Facebook | snapsave.app | — |
| YouTube Shorts | yt-dlp | — |

## Установка

### Зависимости

- Go 1.21+
- `yt-dlp` — для YouTube Shorts (`apt install yt-dlp` или `pip install yt-dlp`)
- `ffprobe` (из пакета ffmpeg) — для определения размеров видео (`apt install ffmpeg`)

### Локальная сборка

```bash
git clone https://github.com/memrook/VideoSaverBot.git
cd VideoSaverBot
go mod tidy
go build -o videosaverbot
```

### Запуск

```bash
TELEGRAM_BOT_TOKEN="your_token" ./videosaverbot
# или
./videosaverbot -token="your_token" -debug=true -concurrent=10
```

Переменные окружения:

| Переменная | Описание |
|-----------|----------|
| `TELEGRAM_BOT_TOKEN` | Токен бота (обязательно) |
| `BOT_ADMIN_ID` | Telegram user ID администратора для команды `/stats` |

### Развертывание на сервере (systemd)

```bash
chmod +x deploy.sh
sudo ./deploy.sh
```

Скрипт установит зависимости, соберёт бинарник, создаст systemd-сервис и пользователя.

Управление сервисом:

```bash
systemctl start/stop/restart videosaverbot
systemctl status videosaverbot
journalctl -u videosaverbot -f
```

Конфигурация сервиса: `/etc/videosaverbot/token.conf`

```
TELEGRAM_BOT_TOKEN=...
BOT_ADMIN_ID=123456789
```

## Использование

1. Отправьте команду `/start` или `/help`
2. Вставьте ссылку на видео из поддерживаемой платформы
3. Бот скачает и пришлёт видео

В групповых чатах бот реагирует только на чистые ссылки, @упоминание или команды.

### Команды

| Команда | Описание |
|---------|----------|
| `/start` | Приветствие |
| `/help` | Инструкция по использованию |
| `/stats` | Статистика (только для `BOT_ADMIN_ID`) |

## Структура проекта

```
main.go                    — точка входа, роутинг, semaphore, graceful shutdown
downloader/downloader.go   — вся логика скачивания
go.mod / go.sum            — зависимости
deploy.sh                  — скрипт развёртывания на Ubuntu
```

## Лицензия

MIT
