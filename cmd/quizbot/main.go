
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"gitlab.com/slon/shad-go/Exam-1-QuizBot/quizbot/internal/quiz"
	"gitlab.com/slon/shad-go/Exam-1-QuizBot/quizbot/internal/telegram"
)

func main() {
	token := flag.String("token", "", "Telegram bot token")
	username := flag.String("bot-username", "", "Telegram bot username")
	flag.Parse()
	*token = strings.TrimSpace(*token)
	*username = strings.TrimPrefix(strings.TrimSpace(*username), "@")
	if *token == "" || *username == "" {
		fmt.Fprintln(os.Stderr, "--token and --bot-username are required")
		os.Exit(2)
	}
	client := telegram.NewHTTPClient(*token)
	engine := quiz.NewEngine()
	bot := telegram.NewBot(client, engine, *username)
	if err := bot.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
