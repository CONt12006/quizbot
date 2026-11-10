<p align="center">
  <a href="README.md"><img alt="English" src="https://img.shields.io/badge/English-1f6feb?style=for-the-badge"></a>
  <a href="README.ru.md"><img alt="Русский" src="https://img.shields.io/badge/%D0%A0%D1%83%D1%81%D1%81%D0%BA%D0%B8%D0%B9-555555?style=for-the-badge"></a>
</p>

# QuizBot

**QuizBot** is a Telegram bot for running interactive quizzes, written in **Go**.

The host uploads a quiz as JSON, creates a game session and invites participants with a Telegram link. The bot runs timed questions, collects answers, calculates results and builds the final leaderboard.

## Features

* uploading and validating quizzes from JSON;
* separate game sessions;
* inviting participants via Telegram deep links;
* lobby and participant registration;
* a timer for each question;
* automatic advance once all participants have answered;
* shuffled questions and answer options;
* scoring by points and answer time;
* leaderboard;
* re-running saved quizzes;
* history of past games;
* exporting results to CSV.

## Tech stack

* **Go**
* **Telegram Bot API**
* **goroutines & channels**
* **context.Context**
* **sync.RWMutex**
* **net/http**
* **JSON**

## Architecture

```text
quizbot/
├── cmd/quizbot/       # application entry point
├── interval/
│   ├── quiz/          # quiz business logic
│   ├── storage/       # storage layer
│   └── telegram/      # Telegram API and handlers
└── go.mod
```

The core business logic is decoupled from the Telegram API and the storage layer through interfaces.

```text
Telegram
    ↓
Handlers
    ↓
Quiz Engine
    ↓
Storage
```

## Concurrency

Game sessions run asynchronously using **goroutines**, **channels** and timers.

```go
go e.runQuizLoop(ctx, runID, events)
```

Quiz and participant state is protected with:

```go
sync.RWMutex
```

`context.Context` manages lifecycle and cancellation.

## Leaderboard

A leaderboard is built when the quiz ends.

Ranking criteria:

1. points scored;
2. total answer time;
3. Telegram ID as an extra tie-breaker.

With equal points, the participant who answered faster ranks higher.


## Running

```bash
git clone <repository-url>
cd quizbot

go mod download

go run ./cmd/quizbot \
  --token "<TELEGRAM_BOT_TOKEN>" \
  --bot-username "<BOT_USERNAME>"
```

Once it is running, send the bot a JSON file with the quiz configuration.
