//go:build !solution

package quiz

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	mathrand "math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	mu      sync.RWMutex
	quizzes map[string]*Quiz
	runs    map[string]*runState
}

type runState struct {
	run             *QuizRun
	quiz            *Quiz
	currentQuestion int
	questionStarted time.Time
	questionDone    chan struct{}
	answered        map[int64]bool
	results         *QuizResults
	questionStarts  map[int]time.Time
}

func NewEngine() *Engine {
	return &Engine{quizzes: make(map[string]*Quiz), runs: make(map[string]*runState)}
}

func (e *Engine) LoadQuiz(data []byte) (*Quiz, error) {
	var raw struct {
		Title     string     `json:"title"`
		Settings  Settings   `json:"settings"`
		Questions []Question `json:"questions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse quiz: %w", err)
	}
	raw.Title = strings.TrimSpace(raw.Title)
	if raw.Title == "" {
		return nil, errors.New("quiz title is required")
	}
	if raw.Settings.TimePerQuestion <= 0 {
		return nil, errors.New("time_per_question must be positive")
	}
	if raw.Settings.MaxParticipants < 0 {
		return nil, errors.New("max_participants cannot be negative")
	}
	if len(raw.Questions) == 0 {
		return nil, errors.New("quiz must contain at least one question")
	}
	for i := range raw.Questions {
		q := &raw.Questions[i]
		q.Text = strings.TrimSpace(q.Text)
		if q.Text == "" {
			return nil, fmt.Errorf("question %d text is required", i+1)
		}
		if len(q.Options) < 2 || len(q.Options) > len(AnswerLetters) {
			return nil, fmt.Errorf("question %d must have 2-%d options", i+1, len(AnswerLetters))
		}
		for j := range q.Options {
			q.Options[j] = strings.TrimSpace(q.Options[j])
			if q.Options[j] == "" {
				return nil, fmt.Errorf("question %d option %d is empty", i+1, j+1)
			}
		}
		if q.Correct < 0 || q.Correct >= len(q.Options) {
			return nil, fmt.Errorf("question %d correct index is out of range", i+1)
		}
		if q.Points < 0 {
			return nil, fmt.Errorf("question %d points cannot be negative", i+1)
		}
		if q.Points == 0 {
			q.Points = 1
		}
		if q.Time < 0 {
			return nil, fmt.Errorf("question %d time cannot be negative", i+1)
		}
	}
	quiz := &Quiz{
		ID: newID(), Title: raw.Title, Settings: raw.Settings,
		Questions: cloneQuestions(raw.Questions), CreatedAt: time.Now(),
	}
	e.mu.Lock()
	e.quizzes[quiz.ID] = cloneQuiz(quiz)
	e.mu.Unlock()
	return cloneQuiz(quiz), nil
}

func (e *Engine) StartRun(ctx context.Context, q *Quiz) (*QuizRun, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q == nil || q.ID == "" {
		return nil, errors.New("invalid quiz")
	}
	runtimeQuiz := cloneQuiz(q)
	prepareQuiz(runtimeQuiz)
	run := &QuizRun{ID: newID(), QuizID: q.ID, Status: RunStatusLobby, Participants: make(map[int64]*Participant), Answers: make(map[int64][]Answer)}
	state := &runState{run: run, quiz: runtimeQuiz, currentQuestion: -1, answered: make(map[int64]bool), questionStarts: make(map[int]time.Time)}
	e.mu.Lock()
	e.quizzes[q.ID] = cloneQuiz(q)
	e.runs[run.ID] = state
	e.mu.Unlock()
	return cloneRun(run), nil
}

func (e *Engine) JoinRun(ctx context.Context, runID string, participant *Participant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if participant == nil || participant.TelegramID == 0 {
		return errors.New("invalid participant")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state, ok := e.runs[runID]
	if !ok {
		return errors.New("run not found")
	}
	if state.run.Status == RunStatusFinished {
		return errors.New("run is not accepting participants")
	}
	if _, exists := state.run.Participants[participant.TelegramID]; exists {
		return errors.New("participant already joined")
	}
	if limit := state.quiz.Settings.MaxParticipants; limit > 0 && len(state.run.Participants) >= limit {
		return errors.New("participant limit reached")
	}
	p := cloneParticipant(participant)
	if p.JoinedAt.IsZero() {
		p.JoinedAt = time.Now()
	}
	state.run.Participants[p.TelegramID] = p
	state.run.Answers[p.TelegramID] = []Answer{}
	return nil
}

func (e *Engine) GetParticipantCount(runID string) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	state, ok := e.runs[runID]
	if !ok {
		return 0
	}
	return len(state.run.Participants)
}

func (e *Engine) StartQuiz(ctx context.Context, runID string) (<-chan QuizEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	state, ok := e.runs[runID]
	if !ok {
		e.mu.Unlock()
		return nil, errors.New("run not found")
	}
	if state.run.Status != RunStatusLobby {
		e.mu.Unlock()
		return nil, errors.New("run is not in lobby")
	}
	state.run.Status = RunStatusRunning
	state.run.StartedAt = time.Now()
	e.mu.Unlock()
	events := make(chan QuizEvent, 4)
	go e.runQuizLoop(ctx, runID, events)
	return events, nil
}

func (e *Engine) runQuizLoop(ctx context.Context, runID string, events chan<- QuizEvent) {
	defer close(events)
	e.mu.RLock()
	state, ok := e.runs[runID]
	if !ok {
		e.mu.RUnlock()
		return
	}
	count := len(state.quiz.Questions)
	e.mu.RUnlock()
	for idx := 0; idx < count; idx++ {
		e.mu.Lock()
		state = e.runs[runID]
		if state == nil || state.run.Status != RunStatusRunning {
			e.mu.Unlock()
			return
		}
		state.currentQuestion = idx
		state.questionStarted = time.Now()
		state.questionStarts[idx] = state.questionStarted
		state.questionDone = make(chan struct{})
		state.answered = make(map[int64]bool)
		q := cloneQuestion(&state.quiz.Questions[idx])
		duration := questionDuration(state.quiz, idx)
		done := state.questionDone
		e.mu.Unlock()

		if !sendEvent(ctx, events, QuizEvent{Type: EventTypeQuestion, QuestionIdx: idx, Question: q, TimeLeft: duration}) {
			e.cancelRun(runID)
			return
		}
		timer := time.NewTimer(duration)
		timedOut := false
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			e.cancelRun(runID)
			return
		case <-done:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
			timedOut = true
		}
		if timedOut {
			if !sendEvent(ctx, events, QuizEvent{Type: EventTypeTimeUp, QuestionIdx: idx, Question: q}) {
				e.cancelRun(runID)
				return
			}
		}
	}
	e.mu.Lock()
	state = e.runs[runID]
	if state == nil {
		e.mu.Unlock()
		return
	}
	state.currentQuestion = -1
	state.run.Status = RunStatusFinished
	state.run.FinishedAt = time.Now()
	state.results = e.buildResultsLocked(state)
	result := cloneResults(state.results)
	e.mu.Unlock()
	sendEvent(ctx, events, QuizEvent{Type: EventTypeFinished, QuestionIdx: -1, Results: result})
}

func (e *Engine) SubmitAnswer(ctx context.Context, runID string, participantID int64, questionIdx int, answerIdx int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state, ok := e.runs[runID]
	if !ok {
		return errors.New("run not found")
	}
	if state.run.Status != RunStatusRunning {
		return errors.New("quiz is not running")
	}
	if _, ok := state.run.Participants[participantID]; !ok {
		return errors.New("participant not found")
	}
	if questionIdx < 0 || questionIdx >= len(state.quiz.Questions) {
		return errors.New("question index out of range")
	}
	if questionIdx != state.currentQuestion {
		return errors.New("question is not current")
	}
	q := &state.quiz.Questions[questionIdx]
	if answerIdx < 0 || answerIdx >= len(q.Options) {
		return errors.New("answer index out of range")
	}
	if state.answered[participantID] {
		return errors.New("answer already submitted")
	}
	now := time.Now()
	state.run.Answers[participantID] = append(state.run.Answers[participantID], Answer{QuestionIdx: questionIdx, AnswerIdx: answerIdx, IsCorrect: answerIdx == q.Correct, Points: pointsFor(q, answerIdx == q.Correct), AnsweredAt: now})
	state.answered[participantID] = true
	if len(state.answered) == len(state.run.Participants) && state.questionDone != nil {
		close(state.questionDone)
		state.questionDone = nil
	}
	return nil
}

func (e *Engine) SubmitAnswerByLetter(ctx context.Context, runID string, participantID int64, letter string) error {
	idx, ok := LetterToIndex(strings.ToUpper(strings.TrimSpace(letter)))
	if !ok {
		return errors.New("invalid answer letter")
	}
	questionIdx := e.GetCurrentQuestion(runID)
	if questionIdx < 0 {
		return errors.New("no active question")
	}
	return e.SubmitAnswer(ctx, runID, participantID, questionIdx, idx)
}

func (e *Engine) GetCurrentQuestion(runID string) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	state, ok := e.runs[runID]
	if !ok || state.run.Status != RunStatusRunning {
		return -1
	}
	return state.currentQuestion
}

func (e *Engine) GetResults(runID string) (*QuizResults, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	state, ok := e.runs[runID]
	if !ok {
		return nil, errors.New("run not found")
	}
	if state.run.Status != RunStatusFinished || state.results == nil {
		return nil, errors.New("quiz is not finished")
	}
	return cloneResults(state.results), nil
}

func (e *Engine) ExportCSV(runID string) ([]byte, error) {
	results, err := e.GetResults(runID)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"Rank", "TelegramID", "Username", "FirstName", "LastName", "Score", "CorrectCount", "TotalTime"}); err != nil {
		return nil, err
	}
	for _, entry := range results.Leaderboard {
		p := entry.Participant
		row := []string{strconv.Itoa(entry.Rank), strconv.FormatInt(p.TelegramID, 10), p.Username, p.FirstName, p.LastName, strconv.Itoa(entry.Score), strconv.Itoa(entry.CorrectCount), entry.TotalTime.String()}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (e *Engine) GetRun(runID string) (*QuizRun, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	state, ok := e.runs[runID]
	if !ok {
		return nil, errors.New("run not found")
	}
	return cloneRun(state.run), nil
}

func (e *Engine) buildResultsLocked(state *runState) *QuizResults {
	entries := make([]LeaderboardEntry, 0, len(state.run.Participants))
	for id, participant := range state.run.Participants {
		byQuestion := make(map[int]Answer)
		for _, a := range state.run.Answers[id] {
			byQuestion[a.QuestionIdx] = a
		}
		entry := LeaderboardEntry{Participant: cloneParticipant(participant)}
		for idx := range state.quiz.Questions {
			duration := questionDuration(state.quiz, idx)
			if a, ok := byQuestion[idx]; ok {
				if a.IsCorrect {
					entry.CorrectCount++
					entry.Score += a.Points
				}
				start := state.questionStarts[idx]
				response := a.AnsweredAt.Sub(start)
				if response < 0 {
					response = 0
				}
				if response > duration {
					response = duration
				}
				entry.TotalTime += response
			} else {
				entry.TotalTime += duration
			}
		}
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score
		}
		if entries[i].TotalTime != entries[j].TotalTime {
			return entries[i].TotalTime < entries[j].TotalTime
		}
		return entries[i].Participant.TelegramID < entries[j].Participant.TelegramID
	})
	for i := range entries {
		entries[i].Rank = i + 1
	}
	total := state.run.FinishedAt.Sub(state.run.StartedAt)
	if total < 0 {
		total = 0
	}
	return &QuizResults{RunID: state.run.ID, QuizTitle: state.quiz.Title, Leaderboard: entries, TotalTime: total}
}

func (e *Engine) cancelRun(runID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state := e.runs[runID]; state != nil && state.run.Status == RunStatusRunning {
		state.currentQuestion = -1
		state.run.Status = RunStatusFinished
		state.run.FinishedAt = time.Now()
		state.results = e.buildResultsLocked(state)
	}
}

func questionDuration(q *Quiz, idx int) time.Duration {
	seconds := q.Settings.TimePerQuestion
	if q.Questions[idx].Time > 0 {
		seconds = q.Questions[idx].Time
	}
	return time.Duration(seconds) * time.Second
}

func pointsFor(q *Question, correct bool) int {
	if !correct {
		return 0
	}
	if q.Points > 0 {
		return q.Points
	}
	return 1
}

func sendEvent(ctx context.Context, ch chan<- QuizEvent, event QuizEvent) bool {
	select {
	case ch <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func prepareQuiz(q *Quiz) {
	r := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	if q.Settings.ShuffleQuestions {
		r.Shuffle(len(q.Questions), func(i, j int) { q.Questions[i], q.Questions[j] = q.Questions[j], q.Questions[i] })
	}
	for i := range q.Questions {
		shuffle := q.Settings.ShuffleAnswers
		if q.Questions[i].Shuffle != nil {
			shuffle = *q.Questions[i].Shuffle
		}
		if !shuffle {
			continue
		}
		oldCorrect := q.Questions[i].Correct
		type option struct {
			text    string
			correct bool
		}
		opts := make([]option, len(q.Questions[i].Options))
		for j, text := range q.Questions[i].Options {
			opts[j] = option{text: text, correct: j == oldCorrect}
		}
		r.Shuffle(len(opts), func(a, b int) { opts[a], opts[b] = opts[b], opts[a] })
		for j := range opts {
			q.Questions[i].Options[j] = opts[j].text
			if opts[j].correct {
				q.Questions[i].Correct = j
			}
		}
	}
}

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func cloneQuiz(q *Quiz) *Quiz {
	if q == nil {
		return nil
	}
	out := *q
	out.Settings.Registration = append([]string(nil), q.Settings.Registration...)
	out.Questions = cloneQuestions(q.Questions)
	return &out
}
func cloneQuestions(in []Question) []Question {
	out := make([]Question, len(in))
	for i := range in {
		out[i] = *cloneQuestion(&in[i])
	}
	return out
}
func cloneQuestion(q *Question) *Question {
	if q == nil {
		return nil
	}
	out := *q
	out.Options = append([]string(nil), q.Options...)
	if q.Shuffle != nil {
		v := *q.Shuffle
		out.Shuffle = &v
	}
	return &out
}
func cloneParticipant(p *Participant) *Participant {
	if p == nil {
		return nil
	}
	out := *p
	if p.RegData != nil {
		out.RegData = make(map[string]string, len(p.RegData))
		for k, v := range p.RegData {
			out.RegData[k] = v
		}
	}
	return &out
}
func cloneRun(r *QuizRun) *QuizRun {
	if r == nil {
		return nil
	}
	out := *r
	out.Participants = make(map[int64]*Participant, len(r.Participants))
	for k, v := range r.Participants {
		out.Participants[k] = cloneParticipant(v)
	}
	out.Answers = make(map[int64][]Answer, len(r.Answers))
	for k, v := range r.Answers {
		out.Answers[k] = append([]Answer(nil), v...)
	}
	return &out
}
func cloneResults(r *QuizResults) *QuizResults {
	if r == nil {
		return nil
	}
	out := *r
	out.Leaderboard = make([]LeaderboardEntry, len(r.Leaderboard))
	for i, v := range r.Leaderboard {
		out.Leaderboard[i] = v
		out.Leaderboard[i].Participant = cloneParticipant(v.Participant)
	}
	return &out
}
