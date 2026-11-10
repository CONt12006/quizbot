<p align="center">
  <a href="README.md"><img alt="English" src="https://img.shields.io/badge/English-555555?style=for-the-badge"></a>
  <a href="README.ru.md"><img alt="Русский" src="https://img.shields.io/badge/%D0%A0%D1%83%D1%81%D1%81%D0%BA%D0%B8%D0%B9-1f6feb?style=for-the-badge"></a>
</p>

# QuizBot

**QuizBot** — Telegram-бот для проведения интерактивных квизов, написанный на **Go**.

Организатор загружает квиз в JSON, создаёт игровую сессию и приглашает участников по Telegram-ссылке. Бот проводит вопросы по таймеру, принимает ответы, рассчитывает результаты и формирует итоговый рейтинг.

## Возможности

* загрузка и валидация квизов из JSON;
* создание отдельных игровых сессий;
* приглашение участников через Telegram deep links;
* lobby и регистрация участников;
* таймер для каждого вопроса;
* автоматический переход после ответа всех участников;
* перемешивание вопросов и вариантов ответа;
* подсчёт баллов и времени ответа;
* leaderboard;
* повторный запуск сохранённых квизов;
* история проведённых игр;
* экспорт результатов в CSV.

## Технологии

* **Go**
* **Telegram Bot API**
* **goroutines & channels**
* **context.Context**
* **sync.RWMutex**
* **net/http**
* **JSON**

## Архитектура

```text
quizbot/
├── cmd/quizbot/       # запуск приложения
├── interval/
│   ├── quiz/          # бизнес-логика квиза
│   ├── storage/       # слой хранения данных
│   └── telegram/      # Telegram API и handlers
└── go.mod
```

Основная бизнес-логика отделена от Telegram API и слоя хранения данных через интерфейсы.

```text
Telegram
    ↓
Handlers
    ↓
Quiz Engine
    ↓
Storage
```

## Конкурентность

Игровые сессии работают асинхронно с использованием **goroutines**, **channels** и таймеров.

```go
go e.runQuizLoop(ctx, runID, events)
```

Состояние квизов и участников защищено через:

```go
sync.RWMutex
```

`context.Context` используется для управления жизненным циклом и отмены выполнения.

## Рейтинг

После завершения квиза формируется leaderboard.

Учитываются:

1. количество набранных баллов;
2. суммарное время ответа;
3. Telegram ID как дополнительный tie-breaker.

При одинаковом количестве баллов выше располагается участник, ответивший быстрее.


## Запуск

```bash
git clone <repository-url>
cd quizbot

go mod download

go run ./cmd/quizbot \
  --token "<TELEGRAM_BOT_TOKEN>" \
  --bot-username "<BOT_USERNAME>"
```

После запуска отправьте боту JSON-файл с конфигурацией квиза.
