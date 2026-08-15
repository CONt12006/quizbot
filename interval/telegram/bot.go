package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"gitlab.com/slon/shad-go/Exam-1-QuizBot/quizbot/internal/quiz"
	"gitlab.com/slon/shad-go/Exam-1-QuizBot/quizbot/internal/storage"
)

type registrationState struct {
	runID  string
	fields []string
	index  int
	data   map[string]string
	user   *User
}

type Bot struct {
	client        Client
	engine        quiz.QuizEngine
	store         storage.Storage
	botUsername   string
	mu            sync.RWMutex
	owners        map[string]int64
	userRuns      map[int64]string
	registrations map[int64]*registrationState
}

func NewBot(client Client, engine quiz.QuizEngine, botUsername string) *Bot {
	return NewBotWithStorage(client, engine, storage.NewMemoryStorage(), botUsername)
}

func NewBotWithStorage(client Client, engine quiz.QuizEngine, store storage.Storage, botUsername string) *Bot {
	if store == nil {
		store = storage.NewMemoryStorage()
	}
	return &Bot{
		client: client, engine: engine, store: store, botUsername: botUsername,
		owners: make(map[string]int64), userRuns: make(map[int64]string), registrations: make(map[int64]*registrationState),
	}
}

func (b *Bot) Run() error {
	offset := 0
	for {
		updates, err := b.client.GetUpdates(offset, 30)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			if err := b.HandleUpdate(update); err != nil {
				if update.Message != nil {
					_, _ = b.client.SendMessage(update.Message.Chat.ID, "Ошибка: "+err.Error(), nil)
				}
			}
		}
	}
}

func (b *Bot) HandleUpdate(update Update) error {
	if update.CallbackQuery != nil {
		return b.handleCallback(update.CallbackQuery)
	}
	if update.Message == nil {
		return nil
	}
	msg := update.Message
	if msg.Document != nil {
		return b.handleDocument(msg)
	}
	text := strings.TrimSpace(msg.Text)
	if strings.HasPrefix(text, "/start") {
		return b.handleStart(msg, text)
	}
	if text == "/my_quizzes" {
		return b.handleMyQuizzes(msg)
	}
	if b.hasRegistration(msg) {
		return b.handleRegistration(msg, text)
	}
	if len(text) == 1 {
		letter := strings.ToUpper(text)
		if _, ok := quiz.LetterToIndex(letter); ok {
			return b.handleAnswer(msg, letter)
		}
	}
	_, err := b.client.SendMessage(msg.Chat.ID, "Отправьте JSON-файл квиза, используйте /my_quizzes или перейдите по ссылке приглашения.", nil)
	return err
}

func (b *Bot) handleDocument(msg *Message) error {
	if msg.From == nil {
		return nil
	}
	path, err := b.client.GetFile(msg.Document.FileID)
	if err != nil {
		return err
	}
	data, err := b.client.DownloadFile(path)
	if err != nil {
		return err
	}
	q, err := b.engine.LoadQuiz(data)
	if err != nil {
		_, sendErr := b.client.SendMessage(msg.Chat.ID, "Не удалось загрузить квиз: "+err.Error(), nil)
		return sendErr
	}
	q.OwnerID = msg.From.ID
	if err := b.store.SaveQuiz(context.Background(), q); err != nil {
		return err
	}
	return b.createLobby(msg.Chat.ID, msg.From.ID, q)
}

func (b *Bot) createLobby(chatID, ownerID int64, q *quiz.Quiz) error {
	run, err := b.engine.StartRun(context.Background(), q)
	if err != nil {
		return err
	}
	if err := b.store.SaveRun(context.Background(), run); err != nil {
		return err
	}
	b.mu.Lock()
	b.owners[run.ID] = ownerID
	b.mu.Unlock()
	link := fmt.Sprintf("https://t.me/%s?start=join_%s", b.botUsername, run.ID)
	opts := &SendOptions{ReplyMarkup: &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Начать квиз", CallbackData: "start_" + run.ID}}}}}
	lobby, err := b.client.SendMessage(chatID, fmt.Sprintf("Квиз: %s\nУчастников: 0\nСсылка: %s", q.Title, link), opts)
	if err != nil {
		return err
	}
	if lobby != nil {
		go b.refreshLobby(run.ID, chatID, lobby.MessageID, q.Title, link, opts)
	}
	return nil
}

func (b *Bot) handleStart(msg *Message, text string) error {
	if msg.From == nil {
		return nil
	}
	parts := strings.Fields(text)
	if len(parts) < 2 || !strings.HasPrefix(parts[1], "join_") {
		_, err := b.client.SendMessage(msg.Chat.ID, "Пришлите JSON-файл квиза, используйте /my_quizzes или ссылку приглашения.", nil)
		return err
	}
	runID := strings.TrimPrefix(parts[1], "join_")
	run, err := b.store.GetRun(context.Background(), runID)
	if err != nil {
		_, sendErr := b.client.SendMessage(msg.Chat.ID, "Запуск квиза не найден.", nil)
		return sendErr
	}
	q, err := b.store.GetQuiz(context.Background(), run.QuizID)
	if err != nil {
		return err
	}
	if len(q.Settings.Registration) > 0 {
		state := &registrationState{runID: runID, fields: append([]string(nil), q.Settings.Registration...), data: make(map[string]string), user: msg.From}
		b.mu.Lock()
		b.registrations[msg.From.ID] = state
		b.mu.Unlock()
		_, err := b.client.SendMessage(msg.Chat.ID, "Введите "+state.fields[0]+":", nil)
		return err
	}
	return b.joinParticipant(msg.Chat.ID, runID, msg.From, nil)
}

func (b *Bot) hasRegistration(msg *Message) bool {
	if msg.From == nil {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.registrations[msg.From.ID] != nil
}

func (b *Bot) handleRegistration(msg *Message, text string) error {
	if msg.From == nil {
		return nil
	}
	if text == "" {
		_, err := b.client.SendMessage(msg.Chat.ID, "Значение не может быть пустым.", nil)
		return err
	}
	b.mu.Lock()
	state := b.registrations[msg.From.ID]
	if state == nil {
		b.mu.Unlock()
		return nil
	}
	state.data[state.fields[state.index]] = text
	state.index++
	if state.index < len(state.fields) {
		next := state.fields[state.index]
		b.mu.Unlock()
		_, err := b.client.SendMessage(msg.Chat.ID, "Введите "+next+":", nil)
		return err
	}
	delete(b.registrations, msg.From.ID)
	data := make(map[string]string, len(state.data))
	for k, v := range state.data {
		data[k] = v
	}
	runID := state.runID
	user := state.user
	b.mu.Unlock()
	return b.joinParticipant(msg.Chat.ID, runID, user, data)
}

func (b *Bot) joinParticipant(chatID int64, runID string, user *User, regData map[string]string) error {
	p := &quiz.Participant{TelegramID: user.ID, Username: user.Username, FirstName: user.FirstName, LastName: user.LastName, RegData: regData}
	if err := b.engine.JoinRun(context.Background(), runID, p); err != nil {
		_, sendErr := b.client.SendMessage(chatID, "Не удалось присоединиться: "+err.Error(), nil)
		return sendErr
	}
	if run, err := b.engine.GetRun(runID); err == nil {
		_ = b.store.UpdateRun(context.Background(), run)
	}
	b.mu.Lock()
	b.userRuns[user.ID] = runID
	b.mu.Unlock()
	_, err := b.client.SendMessage(chatID, "Вы присоединились к квизу. Ожидайте начала.", nil)
	return err
}

func (b *Bot) handleMyQuizzes(msg *Message) error {
	if msg.From == nil {
		return nil
	}
	quizzes, err := b.store.ListQuizzes(context.Background(), msg.From.ID)
	if err != nil {
		return err
	}
	if len(quizzes) == 0 {
		_, err := b.client.SendMessage(msg.Chat.ID, "У вас пока нет сохранённых квизов.", nil)
		return err
	}
	for _, q := range quizzes {
		opts := &SendOptions{ReplyMarkup: &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "Провести", CallbackData: "rerun_" + q.ID}, {Text: "Скачать JSON", CallbackData: "json_" + q.ID}},
			{{Text: "История", CallbackData: "history_" + q.ID}},
		}}}
		if _, err := b.client.SendMessage(msg.Chat.ID, q.Title, opts); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) handleCallback(cb *CallbackQuery) error {
	if cb == nil {
		return nil
	}
	if err := b.client.AnswerCallback(cb.ID, ""); err != nil {
		return err
	}
	switch {
	case strings.HasPrefix(cb.Data, "start_"):
		return b.startRunCallback(cb, strings.TrimPrefix(cb.Data, "start_"))
	case strings.HasPrefix(cb.Data, "rerun_"):
		return b.rerunCallback(cb, strings.TrimPrefix(cb.Data, "rerun_"))
	case strings.HasPrefix(cb.Data, "json_"):
		return b.jsonCallback(cb, strings.TrimPrefix(cb.Data, "json_"))
	case strings.HasPrefix(cb.Data, "history_"):
		return b.historyCallback(cb, strings.TrimPrefix(cb.Data, "history_"))
	default:
		return nil
	}
}

func (b *Bot) startRunCallback(cb *CallbackQuery, runID string) error {
	b.mu.RLock()
	owner := b.owners[runID]
	b.mu.RUnlock()
	if cb.From == nil || owner == 0 || cb.From.ID != owner {
		return nil
	}
	events, err := b.engine.StartQuiz(context.Background(), runID)
	if err != nil {
		return err
	}
	if run, err := b.engine.GetRun(runID); err == nil {
		_ = b.store.UpdateRun(context.Background(), run)
	}
	go b.broadcastEvents(runID, events)
	return nil
}

func (b *Bot) rerunCallback(cb *CallbackQuery, quizID string) error {
	if cb.From == nil || cb.Message == nil {
		return nil
	}
	q, err := b.store.GetQuiz(context.Background(), quizID)
	if err != nil {
		return err
	}
	if q.OwnerID != cb.From.ID {
		return nil
	}
	return b.createLobby(cb.Message.Chat.ID, cb.From.ID, q)
}

func (b *Bot) jsonCallback(cb *CallbackQuery, quizID string) error {
	if cb.From == nil || cb.Message == nil {
		return nil
	}
	q, err := b.store.GetQuiz(context.Background(), quizID)
	if err != nil {
		return err
	}
	if q.OwnerID != cb.From.ID {
		return nil
	}
	payload := struct {
		Title     string          `json:"title"`
		Settings  quiz.Settings   `json:"settings"`
		Questions []quiz.Question `json:"questions"`
	}{Title: q.Title, Settings: q.Settings, Questions: q.Questions}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if sender, ok := b.client.(interface {
		SendDocument(int64, string, []byte) error
	}); ok {
		return sender.SendDocument(cb.Message.Chat.ID, "quiz.json", data)
	}
	_, err = b.client.SendMessage(cb.Message.Chat.ID, string(data), nil)
	return err
}

func (b *Bot) historyCallback(cb *CallbackQuery, quizID string) error {
	if cb.From == nil || cb.Message == nil {
		return nil
	}
	q, err := b.store.GetQuiz(context.Background(), quizID)
	if err != nil {
		return err
	}
	if q.OwnerID != cb.From.ID {
		return nil
	}
	runs, err := b.store.ListRuns(context.Background(), quizID)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		_, err := b.client.SendMessage(cb.Message.Chat.ID, "История запусков пуста.", nil)
		return err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "История: %s\n", q.Title)
	for i, run := range runs {
		fmt.Fprintf(&sb, "%d. %s — %s — участников: %d\n", i+1, run.ID, run.Status, len(run.Participants))
	}
	_, err = b.client.SendMessage(cb.Message.Chat.ID, sb.String(), nil)
	return err
}

func (b *Bot) handleAnswer(msg *Message, letter string) error {
	if msg.From == nil {
		return nil
	}
	b.mu.RLock()
	runID := b.userRuns[msg.From.ID]
	b.mu.RUnlock()
	if runID == "" {
		_, err := b.client.SendMessage(msg.Chat.ID, "Вы не участвуете в активном квизе.", nil)
		return err
	}
	if err := b.engine.SubmitAnswerByLetter(context.Background(), runID, msg.From.ID, letter); err != nil {
		_, sendErr := b.client.SendMessage(msg.Chat.ID, "Ответ не принят: "+err.Error(), nil)
		return sendErr
	}
	_, err := b.client.SendMessage(msg.Chat.ID, "✅ Ответ принят!", nil)
	return err
}

func (b *Bot) refreshLobby(runID string, chatID int64, messageID int, title, link string, opts *SendOptions) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		run, err := b.engine.GetRun(runID)
		if err != nil || run.Status != quiz.RunStatusLobby {
			return
		}
		text := fmt.Sprintf("Квиз: %s\nУчастников: %d\nСсылка: %s", title, b.engine.GetParticipantCount(runID), link)
		_ = b.client.EditMessage(chatID, messageID, text, opts)
	}
}

func (b *Bot) broadcastEvents(runID string, events <-chan quiz.QuizEvent) {
	for event := range events {
		run, err := b.engine.GetRun(runID)
		if err != nil {
			return
		}
		switch event.Type {
		case quiz.EventTypeQuestion:
			text := formatQuestion(event.QuestionIdx, event.Question, event.TimeLeft)
			for id := range run.Participants {
				_, _ = b.client.SendMessage(id, text, nil)
			}
		case quiz.EventTypeTimeUp:
			for id := range run.Participants {
				_, _ = b.client.SendMessage(id, "⏱ Время вышло.", nil)
			}
		case quiz.EventTypeFinished:
			b.sendResults(runID, event.Results)
		}
	}
}

func (b *Bot) sendResults(runID string, results *quiz.QuizResults) {
	if results == nil {
		return
	}
	if run, err := b.engine.GetRun(runID); err == nil {
		_ = b.store.UpdateRun(context.Background(), run)
	}
	top := results.Leaderboard
	if len(top) > 10 {
		top = top[:10]
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "🏆 Результаты квиза \"%s\"\n\nТоп-10:\n", results.QuizTitle)
	for _, entry := range top {
		name := entry.Participant.Username
		if name != "" {
			name = "@" + name
		} else {
			name = strings.TrimSpace(entry.Participant.FirstName + " " + entry.Participant.LastName)
		}
		if name == "" {
			name = fmt.Sprintf("%d", entry.Participant.TelegramID)
		}
		fmt.Fprintf(&sb, "%d. %s — %d баллов\n", entry.Rank, name, entry.Score)
	}
	for _, entry := range results.Leaderboard {
		_, _ = b.client.SendMessage(entry.Participant.TelegramID, fmt.Sprintf("Ваш результат: %d баллов, место %d\n\n%s", entry.Score, entry.Rank, sb.String()), nil)
	}
	b.mu.RLock()
	owner := b.owners[runID]
	b.mu.RUnlock()
	if owner != 0 {
		_, _ = b.client.SendMessage(owner, sb.String(), nil)
		if sender, ok := b.client.(interface {
			SendDocument(int64, string, []byte) error
		}); ok {
			if data, err := b.engine.ExportCSV(runID); err == nil {
				_ = sender.SendDocument(owner, "results.csv", data)
			}
		}
	}
}

func formatQuestion(idx int, q *quiz.Question, left time.Duration) string {
	if q == nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Вопрос %d\n\n%s\n\n", idx+1, q.Text)
	for i, option := range q.Options {
		fmt.Fprintf(&sb, "%s. %s\n", quiz.IndexToLetter(i), option)
	}
	fmt.Fprintf(&sb, "\n⏱ %d секунд\n\nОтправьте букву ответа (A, B, C, ...)", int(left.Seconds()))
	return sb.String()
}
