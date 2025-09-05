#!/bin/bash

# Скрипт для развертывания VideoSaverBot как systemd сервиса на Ubuntu
# Автор: memrook

set -e  # Прерывать выполнение при любой ошибке

# Цвета для вывода
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # Сброс цвета

# Печать сообщения с форматированием
print_message() {
    echo -e "${GREEN}[VideoSaverBot]${NC} $1"
}

print_error() {
    echo -e "${RED}[ОШИБКА]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[ВНИМАНИЕ]${NC} $1"
}

# Проверка прав администратора
if [ "$EUID" -ne 0 ]; then
    print_error "Этот скрипт нужно запускать с правами администратора (sudo)."
    exit 1
fi

# Параметры установки
APP_NAME="videosaverbot"
REPO_URL="https://github.com/memrook/VideoSaverBot.git"
INSTALL_DIR="/opt/$APP_NAME"
SERVICE_USER="videosaverbot"
TOKEN_FILE="/etc/$APP_NAME/token.conf"
CONFIG_DIR="/etc/$APP_NAME"
CONCURRENT_DOWNLOADS=5
DEBUG_MODE="false"

print_message "Начинаем установку VideoSaverBot на сервер..."

# Проверка наличия необходимых утилит
print_message "Проверка наличия необходимых утилит..."
command -v git >/dev/null 2>&1 || { print_error "Требуется git. Устанавливаем..."; apt-get update && apt-get install -y git; }

# Проверка и установка Go
print_message "Проверка наличия Go..."
if ! command -v go >/dev/null 2>&1; then
    print_warning "Go не установлен. Устанавливаем Go..."
    apt-get update
    apt-get install -y golang
    
    if ! command -v go >/dev/null 2>&1; then
        print_error "Не удалось установить Go через apt. Устанавливаем из официального репозитория..."
        wget https://go.dev/dl/go1.21.3.linux-amd64.tar.gz
        rm -rf /usr/local/go && tar -C /usr/local -xzf go1.21.3.linux-amd64.tar.gz
        echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
        chmod +x /etc/profile.d/go.sh
        source /etc/profile.d/go.sh
        rm go1.21.3.linux-amd64.tar.gz
    fi
fi

# Установка yt-dlp для скачивания YouTube видео
print_message "Проверка и установка yt-dlp..."
if ! command -v yt-dlp >/dev/null 2>&1; then
    print_warning "yt-dlp не установлен. Устанавливаем..."
    # Установка pip если его нет
    if ! command -v pip3 >/dev/null 2>&1; then
        apt-get install -y python3-pip
    fi
    
    # Установка yt-dlp через pip
    pip3 install yt-dlp
    
    # Проверяем установку
    if command -v yt-dlp >/dev/null 2>&1; then
        YT_DLP_VERSION=$(yt-dlp --version)
        print_message "yt-dlp установлен, версия: $YT_DLP_VERSION"
    else
        print_error "Не удалось установить yt-dlp"
        exit 1
    fi
else
    YT_DLP_VERSION=$(yt-dlp --version)
    print_message "yt-dlp уже установлен, версия: $YT_DLP_VERSION"
fi

# Установка ffmpeg для обработки видео (если нужно)
print_message "Проверка и установка ffmpeg..."
if ! command -v ffmpeg >/dev/null 2>&1; then
    print_warning "ffmpeg не установлен. Устанавливаем..."
    apt-get install -y ffmpeg
else
    print_message "ffmpeg уже установлен"
fi

# Проверка версии Go
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
print_message "Установлена версия Go: $GO_VERSION"

# Создание пользователя для сервиса
print_message "Создание системного пользователя для сервиса..."
id -u $SERVICE_USER >/dev/null 2>&1 || useradd --system --no-create-home $SERVICE_USER

# Создание директорий
print_message "Создание директорий для приложения..."
mkdir -p $INSTALL_DIR
mkdir -p $CONFIG_DIR

# Клонирование репозитория
print_message "Клонирование репозитория из GitHub..."
if [ -d "$INSTALL_DIR/.git" ]; then
    print_warning "Репозиторий уже существует. Обновляем..."
    cd $INSTALL_DIR
    git pull
else
    git clone $REPO_URL $INSTALL_DIR
    cd $INSTALL_DIR
fi

# Установка зависимостей Go
print_message "Установка зависимостей Go..."
cd $INSTALL_DIR
go mod tidy
go build -o $APP_NAME

# Создание конфигурационного файла для токена
if [ ! -f "$TOKEN_FILE" ]; then
    print_message "Создание файла конфигурации для токена Telegram..."
    echo "# Токен Telegram бота" > $TOKEN_FILE
    echo "TELEGRAM_BOT_TOKEN=" >> $TOKEN_FILE
    print_warning "Файл $TOKEN_FILE создан. Необходимо вручную добавить токен бота."
else
    print_message "Файл конфигурации для токена уже существует."
fi

# Создание systemd сервиса
print_message "Настройка systemd сервиса..."
cat > /etc/systemd/system/$APP_NAME.service << EOF
[Unit]
Description=VideoSaverBot - Telegram бот для скачивания видео
After=network.target

[Service]
Type=simple
User=$SERVICE_USER
WorkingDirectory=$INSTALL_DIR
EnvironmentFile=$TOKEN_FILE
ExecStart=$INSTALL_DIR/$APP_NAME -concurrent=$CONCURRENT_DOWNLOADS -debug=$DEBUG_MODE
Restart=always
RestartSec=10
StandardOutput=syslog
StandardError=syslog
SyslogIdentifier=$APP_NAME

[Install]
WantedBy=multi-user.target
EOF

# Настройка разрешений
print_message "Настройка разрешений..."
chown -R $SERVICE_USER:$SERVICE_USER $INSTALL_DIR
chmod 750 $INSTALL_DIR
chmod 640 $TOKEN_FILE
chown root:$SERVICE_USER $TOKEN_FILE
chmod 750 $INSTALL_DIR/$APP_NAME

# Перезагрузка systemd и настройка автозапуска
print_message "Настройка автозапуска сервиса..."
systemctl daemon-reload
systemctl enable $APP_NAME.service

print_message "Установка завершена!"
print_warning "Не забудьте добавить токен Telegram бота в файл $TOKEN_FILE"
print_message "Команды управления сервисом:"
print_message "  Запуск:               sudo systemctl start $APP_NAME"
print_message "  Остановка:            sudo systemctl stop $APP_NAME"
print_message "  Перезапуск:           sudo systemctl restart $APP_NAME"
print_message "  Проверка статуса:     sudo systemctl status $APP_NAME"
print_message "  Просмотр логов:       sudo journalctl -u $APP_NAME -f"

# Запрос на добавление токена
read -p "Хотите добавить токен Telegram бота сейчас? (y/n): " ADD_TOKEN

if [ "$ADD_TOKEN" = "y" ] || [ "$ADD_TOKEN" = "Y" ]; then
    read -p "Введите токен Telegram бота: " BOT_TOKEN
    sed -i "s/TELEGRAM_BOT_TOKEN=/TELEGRAM_BOT_TOKEN=$BOT_TOKEN/" $TOKEN_FILE
    print_message "Токен добавлен в конфигурационный файл."
    
    read -p "Запустить сервис сейчас? (y/n): " START_SERVICE
    if [ "$START_SERVICE" = "y" ] || [ "$START_SERVICE" = "Y" ]; then
        systemctl start $APP_NAME.service
        print_message "Сервис запущен! Проверьте статус: sudo systemctl status $APP_NAME"
    else
        print_message "Вы можете запустить сервис позже командой: sudo systemctl start $APP_NAME"
    fi
else
    print_message "Не забудьте добавить токен в файл $TOKEN_FILE перед запуском бота!"
fi

exit 0 