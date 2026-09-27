package game

import (
	"context"
	"errors"
	"fmt"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
)

// ChatModel is the single chat model every stage of a turn runs on.
type ChatModel interface {
	Complete(ctx context.Context, messages []llm.Message, opts llm.CompletionOptions) (llm.Result, error)
}

// errRejected marks a stage whose output never passed validation, as opposed
// to a transport error.
var errRejected = errors.New("検証を通る出力が得られませんでした")

// thinkingBudget is added to MaxTokens on a re-think, since the reasoning
// consumes the same token budget as the answer.
const thinkingBudget = 2048

// stage describes one LLM call in a turn (GM, one NPC, or Support).
type stage struct {
	name      string // session_logs role: "gm" / "npc" / "support"
	opts      llm.CompletionOptions
	retryHint string // appended to the error when asking the model to redo its output
	validate  func(llm.Result) error
}

// runStage calls the model until its output passes validation, at most
// retryLimit+1 times. The first attempt answers directly; later attempts get
// the rejected output and the reason, and are allowed to think first.
// Transport errors are returned immediately — re-thinking cannot fix them.
func (e *Engine) runStage(ctx context.Context, sessionID int64, turn int, st stage, msgs []llm.Message) (llm.Result, int, error) {
	opts := st.opts
	opts.Thinking = false
	var lastErr error
	for attempt := 1; attempt <= e.retryLimit+1; attempt++ {
		res, err := e.llm.Complete(ctx, msgs, opts)
		if err != nil {
			return llm.Result{}, attempt, err
		}
		lastErr = checkFinished(res)
		if lastErr == nil {
			lastErr = st.validate(res)
		}
		if lastErr == nil {
			_ = db.AppendLog(e.db, sessionID, turn, st.name, res.Text)
			return res, attempt, nil
		}
		_ = db.AppendLog(e.db, sessionID, turn, st.name,
			fmt.Sprintf("[NG %d/%d] %v\n%s", attempt, e.retryLimit+1, lastErr, res.Text))

		msgs = append(msgs,
			llm.Message{Role: "assistant", Content: res.Text},
			llm.Message{Role: "user", Content: fmt.Sprintf("エラー: %v\n%s", lastErr, st.retryHint)},
		)
		if !opts.Thinking {
			opts.Thinking = true
			opts.MaxTokens += thinkingBudget
		}
	}
	return llm.Result{}, e.retryLimit + 1, fmt.Errorf("%s: %w: %w", st.name, errRejected, lastErr)
}

func checkFinished(res llm.Result) error {
	if res.FinishReason == "length" {
		return fmt.Errorf("出力が max_tokens で途中で切れています")
	}
	return nil
}
