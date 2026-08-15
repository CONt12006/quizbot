package quiz

import (
	"context"
	"sync"
	"testing"
	"time"
)

func testQuiz(t *testing.T, engine *Engine, settings string, questions string) *Quiz {
	t.Helper()
	q, err := engine.LoadQuiz([]byte(`{"title":"test","settings":` + settings + `,"questions":` + questions + `}`))
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestExtraDefaultsAndLetters(t *testing.T) {
	engine := NewEngine()
	q := testQuiz(t, engine, `{"time_per_question":1}`, `[{"text":"q","options":["a","b"],"correct":0}]`)
	if q.Questions[0].Points != 1 {
		t.Fatalf("points=%d", q.Questions[0].Points)
	}
	run, err := engine.StartRun(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.JoinRun(context.Background(), run.ID, &Participant{TelegramID: 1}); err != nil {
		t.Fatal(err)
	}
	events, err := engine.StartQuiz(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	<-events
	if err := engine.SubmitAnswerByLetter(context.Background(), run.ID, 1, "a"); err != nil {
		t.Fatal(err)
	}
	if err := engine.SubmitAnswerByLetter(context.Background(), run.ID, 1, "A"); err == nil {
		t.Fatal("duplicate answer accepted")
	}
	for range events {
	}
	result, err := engine.GetResults(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Leaderboard[0].Score != 1 {
		t.Fatalf("score=%d", result.Leaderboard[0].Score)
	}
}

func TestExtraParticipantLimitAndConcurrency(t *testing.T) {
	engine := NewEngine()
	q := testQuiz(t, engine, `{"time_per_question":1,"max_participants":20}`, `[{"text":"q","options":["a","b"],"correct":0}]`)
	run, err := engine.StartRun(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := int64(1); i <= 20; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			if err := engine.JoinRun(context.Background(), run.ID, &Participant{TelegramID: id}); err != nil {
				t.Errorf("join %d: %v", id, err)
			}
		}(i)
	}
	wg.Wait()
	if engine.GetParticipantCount(run.ID) != 20 {
		t.Fatalf("count=%d", engine.GetParticipantCount(run.ID))
	}
	if err := engine.JoinRun(context.Background(), run.ID, &Participant{TelegramID: 21}); err == nil {
		t.Fatal("limit not enforced")
	}
	events, err := engine.StartQuiz(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	<-events
	for i := int64(1); i <= 20; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			if err := engine.SubmitAnswer(context.Background(), run.ID, id, 0, 0); err != nil {
				t.Errorf("answer %d: %v", id, err)
			}
		}(i)
	}
	wg.Wait()
	for range events {
	}
}

func TestExtraOverrideAndTimeout(t *testing.T) {
	engine := NewEngine()
	q := testQuiz(t, engine, `{"time_per_question":5}`, `[{"text":"q","options":["a","b"],"correct":1,"points":5,"time":1}]`)
	run, err := engine.StartRun(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.JoinRun(context.Background(), run.ID, &Participant{TelegramID: 1}); err != nil {
		t.Fatal(err)
	}
	events, err := engine.StartQuiz(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	first := <-events
	if first.TimeLeft != time.Second {
		t.Fatalf("time=%v", first.TimeLeft)
	}
	if event := <-events; event.Type != EventTypeTimeUp {
		t.Fatalf("event=%s", event.Type)
	}
	for range events {
	}
	result, err := engine.GetResults(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Leaderboard[0].Score != 0 || result.Leaderboard[0].TotalTime != time.Second {
		t.Fatalf("result=%+v", result.Leaderboard[0])
	}
}

func TestExtraContextCancellation(t *testing.T) {
	engine := NewEngine()
	q := testQuiz(t, engine, `{"time_per_question":10}`, `[{"text":"q","options":["a","b"],"correct":0}]`)
	run, err := engine.StartRun(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.JoinRun(context.Background(), run.ID, &Participant{TelegramID: 1}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	events, err := engine.StartQuiz(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	<-events
	cancel()
	select {
	case _, ok := <-events:
		if ok {
			for range events {
			}
		}
	case <-time.After(time.Second):
		t.Fatal("events did not close")
	}
	current, err := engine.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != RunStatusFinished {
		t.Fatalf("status=%s", current.Status)
	}
}

func TestExtraValidation(t *testing.T) {
	engine := NewEngine()
	cases := []string{
		`{"title":"x","settings":{"time_per_question":1},"questions":[{"text":"","options":["a","b"],"correct":0}]}`,
		`{"title":"x","settings":{"time_per_question":1},"questions":[{"text":"q","options":["a","b","c","d","e","f","g"],"correct":0}]}`,
		`{"title":"x","settings":{"time_per_question":1},"questions":[{"text":"q","options":["a",""],"correct":0}]}`,
		`{"title":"x","settings":{"time_per_question":1},"questions":[{"text":"q","options":["a","b"],"correct":0,"points":-1}]}`,
	}
	for _, data := range cases {
		if _, err := engine.LoadQuiz([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
