
package storage

import (
	"context"
	"errors"
	"sort"
	"sync"

	"gitlab.com/slon/shad-go/Exam-1-QuizBot/quizbot/internal/quiz"
)

type MemoryStorage struct {
	mu      sync.RWMutex
	quizzes map[string]*quiz.Quiz
	runs    map[string]*quiz.QuizRun
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{quizzes: make(map[string]*quiz.Quiz), runs: make(map[string]*quiz.QuizRun)}
}

func (s *MemoryStorage) SaveQuiz(ctx context.Context, q *quiz.Quiz) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q == nil || q.ID == "" {
		return errors.New("invalid quiz")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quizzes[q.ID] = copyQuiz(q)
	return nil
}

func (s *MemoryStorage) GetQuiz(ctx context.Context, id string) (*quiz.Quiz, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	q, ok := s.quizzes[id]
	if !ok {
		return nil, errors.New("quiz not found")
	}
	return copyQuiz(q), nil
}

func (s *MemoryStorage) ListQuizzes(ctx context.Context, ownerID int64) ([]*quiz.Quiz, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*quiz.Quiz, 0)
	for _, q := range s.quizzes {
		if q.OwnerID == ownerID {
			out = append(out, copyQuiz(q))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (s *MemoryStorage) DeleteQuiz(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.quizzes[id]; !ok {
		return errors.New("quiz not found")
	}
	delete(s.quizzes, id)
	return nil
}

func (s *MemoryStorage) SaveRun(ctx context.Context, run *quiz.QuizRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if run == nil || run.ID == "" {
		return errors.New("invalid run")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[run.ID] = copyRun(run)
	return nil
}

func (s *MemoryStorage) GetRun(ctx context.Context, id string) (*quiz.QuizRun, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	if !ok {
		return nil, errors.New("run not found")
	}
	return copyRun(run), nil
}

func (s *MemoryStorage) ListRuns(ctx context.Context, quizID string) ([]*quiz.QuizRun, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*quiz.QuizRun, 0)
	for _, run := range s.runs {
		if run.QuizID == quizID {
			out = append(out, copyRun(run))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out, nil
}

func (s *MemoryStorage) UpdateRun(ctx context.Context, run *quiz.QuizRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if run == nil || run.ID == "" {
		return errors.New("invalid run")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; !ok {
		return errors.New("run not found")
	}
	s.runs[run.ID] = copyRun(run)
	return nil
}

func copyQuiz(q *quiz.Quiz) *quiz.Quiz {
	if q == nil {
		return nil
	}
	out := *q
	out.Settings.Registration = append([]string(nil), q.Settings.Registration...)
	out.Questions = make([]quiz.Question, len(q.Questions))
	for i := range q.Questions {
		out.Questions[i] = q.Questions[i]
		out.Questions[i].Options = append([]string(nil), q.Questions[i].Options...)
		if q.Questions[i].Shuffle != nil {
			v := *q.Questions[i].Shuffle
			out.Questions[i].Shuffle = &v
		}
	}
	return &out
}

func copyParticipant(p *quiz.Participant) *quiz.Participant {
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

func copyRun(r *quiz.QuizRun) *quiz.QuizRun {
	if r == nil {
		return nil
	}
	out := *r
	out.Participants = make(map[int64]*quiz.Participant, len(r.Participants))
	for k, v := range r.Participants {
		out.Participants[k] = copyParticipant(v)
	}
	out.Answers = make(map[int64][]quiz.Answer, len(r.Answers))
	for k, v := range r.Answers {
		out.Answers[k] = append([]quiz.Answer(nil), v...)
	}
	return &out
}
