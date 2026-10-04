package game

import (
	"slices"

	"github.com/kf/sw25-omni-master/pkg/llm"
)

// tableSystemPrompt is the same for every stage, turn and session. Keeping it
// byte-identical lets mlx-vlm's prefix cache (APC) reuse its KV across all
// requests; anything that changes (HP, rules, input) goes after it.
const tableSystemPrompt = `あなたはソード・ワールド2.5のセッションを進行するAIです。
1ターンの中で、GM → 各NPC → 数値計算担当 の順に役を交代して応答します。
直前のユーザーメッセージの【指示】に書かれた役と出力形式だけに従い、その役の出力だけを返してください。
それより前の会話は同じターンの経過です。他の役の発言を自分の出力として繰り返さないでください。
すべて日本語で応答してください。`

// transcript is one turn as a single growing conversation. Each stage sends
// the transcript plus its own instruction, and only the stage's accepted
// answer is recorded, so every request begins with the previous request's
// tokens. On mlx-serve's single resident model this turns the five serial
// stages into one prefill of the turn context plus small increments.
type transcript struct {
	msgs []llm.Message
}

func newTranscript() *transcript {
	return &transcript{msgs: []llm.Message{{Role: "system", Content: tableSystemPrompt}}}
}

// ask returns the messages for a stage: the transcript plus instruction. The
// result never shares spare capacity with the transcript, so a stage's
// re-think turns cannot leak into it.
func (t *transcript) ask(instruction string) []llm.Message {
	return append(slices.Clip(t.msgs), llm.Message{Role: "user", Content: instruction})
}

// record appends a stage's instruction and accepted answer. Rejected stages
// are not recorded; the next stage's instruction simply follows.
func (t *transcript) record(instruction, answer string) {
	t.msgs = append(t.msgs,
		llm.Message{Role: "user", Content: instruction},
		llm.Message{Role: "assistant", Content: answer},
	)
}
