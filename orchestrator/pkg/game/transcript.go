package game

import (
	"slices"

	"github.com/kf/sw25-omni-master/pkg/llm"
)

// transcript is one turn as a single growing conversation. Each stage sends
// the transcript plus its own instruction, and only the stage's accepted
// answer is recorded, so every request begins with the previous request's
// tokens. On mlx-serve's single resident model this turns the five serial
// stages into one prefill of the turn context plus small increments.
type transcript struct {
	msgs []llm.Message
}

// newTranscript starts a turn after the session context (system message and
// recent turns), which every stage of the turn shares as its prefix.
func newTranscript(base []llm.Message) *transcript {
	return &transcript{msgs: slices.Clone(base)}
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
