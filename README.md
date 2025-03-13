# VideoSaverBot для Telegram

Телеграм-бот на Go, который позволяет скачивать видео из постов Instagram и Twitter (X) по предоставленной ссылке.

## Возможности

- Скачивание видео из постов Instagram и Twitter/X
- Использование сервисов VXTwitter и DDInstagram для улучшенного и стабильного скачивания
- Поддержка нескольких резервных сервисов для максимальной стабильности
- Автоматическое удаление служебных сообщений для чистого и удобного интерфейса
- Параллельная обработка запросов от нескольких пользователей одновременно
- Защита от конфликтов при одновременном скачивании несколькими пользователями
- Изолированное хранение временных файлов для каждого пользователя
- Автоматическая очистка временных файлов
- Управление нагрузкой через ограничение максимального количества одновременных скачиваний
- Работа как в личных, так и в групповых чатах
- Умная фильтрация сообщений в группах
- Простой и понятный интерфейс
- Быстрый ответ

## Как это работает

Бот использует следующие технологии для извлечения видео:

- **Twitter/X**: Использует сервис VXTwitter (vxtwitter.com), который преобразует оригинальные ссылки Twitter в более доступный формат для извлечения медиа
- **Instagram**: Использует следующие сервисы (в порядке приоритета):
  1. DDInstagram - основной сервис скачивания
  2. InstaFinsta - первый резервный сервис
  3. IGram.io - дополнительный резервный сервис при ошибках в других сервисах

Для безопасной параллельной работы реализованы:
- Изолированное хранение файлов в отдельных директориях для каждого пользователя
- Гарантированно уникальные имена файлов с использованием случайных идентификаторов
- Синхронизация доступа к файловой системе с помощью мьютексов
- Ограничение максимального количества одновременных скачиваний
- Автоматическая очистка временных файлов для экономии дискового пространства

## Установка

### Локальная установка

1. Клонировать репозиторий:
   ```
   git clone https://github.com/memrook/VideoSaverBot.git
   ```

2. Перейти в директорию проекта:
   ```
   cd VideoSaverBot
   ```

3. Установить зависимости:
   ```
   go mod tidy
   ```

4. Собрать проект:
   ```
   go build -o videosaverbot
   ```

### Развертывание на сервере Ubuntu

Для удобного развертывания на сервере Ubuntu используйте автоматический скрипт установки:

1. Клонировать репозиторий:
   ```
   git clone https://github.com/memrook/VideoSaverBot.git
   ```

2. Перейти в директорию проекта:
   ```
   cd VideoSaverBot
   ```

3. Сделать скрипт развертывания исполняемым:
   ```
   chmod +x deploy.sh
   ```

4. Запустить скрипт с правами администратора:
   ```
   sudo ./deploy.sh
   ```

Скрипт выполнит все необходимые действия:
- Установит необходимые зависимости (Git, Go)
- Клонирует репозиторий в `/opt/videosaverbot`
- Соберет проект
- Создаст системного пользователя для запуска сервиса
- Создаст и настроит systemd сервис для автозапуска
- Предложит добавить токен бота и запустить сервис

#### Управление сервисом

После установки для управления ботом используйте стандартные команды systemd:

- Запуск бота: `sudo systemctl start videosaverbot`
- Остановка бота: `sudo systemctl stop videosaverbot`
- Перезапуск бота: `sudo systemctl restart videosaverbot` 
- Проверка статуса: `sudo systemctl status videosaverbot`
- Просмотр логов: `sudo journalctl -u videosaverbot -f`

## Использование

1. Начните чат с ботом в Telegram.
2. Отправьте команду `/start` для получения приветственного сообщения.
3. Скопируйте ссылку на пост Instagram или Twitter, содержащий видео.
4. Отправьте ссылку боту.
5. Бот скачает и отправит вам видео.

Бот может обрабатывать запросы от нескольких пользователей одновременно.

### Использование в групповых чатах

Бот оптимизирован для работы в групповых чатах и реагирует только на:

1. Чистые ссылки на Instagram или Twitter/X (сообщения, содержащие только ссылку)
2. Сообщения с упоминанием бота (например, `@videosaverbot`)
3. Прямые команды (`/start`, `/help`)

Это позволяет использовать бота в оживленных группах без лишнего спама.

## Запуск с параметрами

Бот может быть запущен с дополнительными параметрами командной строки:

- `-token="ВАШ_ТОКЕН"` - Указание токена бота непосредственно в командной строке
- `-debug=true` - Включение режима отладки (по умолчанию отключен)
- `-concurrent=10` - Установка максимального количества одновременных скачиваний (по умолчанию 5)

Пример:
```
./videosaverbot -token="1234567890:ABCDEFGHIJKLMNOPQRSTUVWXYZ" -debug=true -concurrent=10
```

Также токен бота может быть установлен через переменную окружения TELEGRAM_BOT_TOKEN:
```
export TELEGRAM_BOT_TOKEN="1234567890:ABCDEFGHIJKLMNOPQRSTUVWXYZ"
./videosaverbot
```

## Структура проекта

Проект имеет следующую структуру:

- `main.go` - Основной файл с логикой бота
- `downloader/downloader.go` - Модуль для скачивания видео из Instagram и Twitter
- `go.mod` и `go.sum` - Файлы для управления зависимостями
- `README.md` - Документация проекта
- `deploy.sh` - Скрипт для автоматического развертывания на сервере Ubuntu

## Лицензия

MIT 
