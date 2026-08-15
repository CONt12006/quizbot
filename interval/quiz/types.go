package quiz

import (
	"context"
	"time"
)

type Quiz struct {
	ID        string
	OwnerID   int64
	Title     string
	Settings  Settings
	Questions []Question
	CreatedAt time.Time
}

type Settings struct {
	TimePerQuestion  int      `json:"time_per_question"`
	ShuffleQuestions bool     `json:"shuffle_questions"`
	ShuffleAnswers   bool     `json:"shuffle_answers"`
	MaxParticipants  int      `json:"max_participants"`
	Registration     []string `json:"registration"`
}

type Question struct {
	Text        string   `json:"text"`
	Options     []string `json:"options"`
	Correct     int      `json:"correct"`
	Explanation string   `json:"explanation"`
	Points      int      `json:"points"`
	Time        int      `json:"time"`
	Shuffle     *bool    `json:"shuffle"`
}

type QuizRun struct { //nolint:revive
	ID           string
	QuizID       string
	Status       RunStatus
	Participants map[int64]*Participant
	Answers      map[int64][]Answer
	StartedAt    time.Time
	FinishedAt   time.Time
}

type RunStatus string

const (
	RunStatusLobby    RunStatus = "lobby"
	RunStatusRunning  RunStatus = "running"
	RunStatusFinished RunStatus = "finished"
)

type Participant struct {
	TelegramID int64
	Username   string
	FirstName  string
	LastName   string
	RegData    map[string]string
	JoinedAt   time.Time
}

type Answer struct {
	QuestionIdx int
	AnswerIdx   int
	IsCorrect   bool
	Points      int
	AnsweredAt  time.Time
}

type QuizResults struct { //nolint:revive
	RunID       string
	QuizTitle   string
	Leaderboard []LeaderboardEntry
	TotalTime   time.Duration
}

type LeaderboardEntry struct {
	Participant  *Participant
	Score        int
	CorrectCount int
	TotalTime    time.Duration
	Rank         int
}

type QuizEngine interface { //nolint:revive
	LoadQuiz(data []byte) (*Quiz, error)

	StartRun(ctx context.Context, quiz *Quiz) (*QuizRun, error)

	JoinRun(ctx context.Context, runID string, participant *Participant) error

	GetParticipantCount(runID string) int

	StartQuiz(ctx context.Context, runID string) (<-chan QuizEvent, error)

	SubmitAnswer(
		ctx context.Context,
		runID string,
		participantID int64,
		questionIdx int,
		answerIdx int,
	) error


	SubmitAnswerByLetter(
		ctx context.Context,
		runID string,
		participantID int64,
		letter string,
	) error

	GetCurrentQuestion(runID string) int

	GetResults(runID string) (*QuizResults, error)

	ExportCSV(runID string) ([]byte, error)

	GetRun(runID string) (*QuizRun, error)
}

type QuizEvent struct { //nolint:revive
	Type        EventType
	QuestionIdx int
	Question    *Question
	TimeLeft    time.Duration
	Results     *QuizResults
}

type EventType string

const (
	EventTypeQuestion EventType = "question"
	EventTypeTimeUp   EventType = "time_up"
	EventTypeFinished EventType = "finished"
)

var AnswerLetters = []string{"A", "B", "C", "D", "E", "F"}

func LetterToIndex(letter string) (int, bool) {
	for i, l := range AnswerLetters {
		if l == letter {
			return i, true
		}
	}

	return -1, false
}

// IndexToLetter преобразует индекс в букву (0=A, 1=B, ...).
func IndexToLetter(idx int) string {
	if idx >= 0 && idx < len(AnswerLetters) {
		return AnswerLetters[idx]
	}

	return ""
}
