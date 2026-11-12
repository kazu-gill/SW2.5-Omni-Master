package game

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
)

// summaryEvery is how many turns, beyond the ones sent word for word
// (recentTurns), wait before they are folded into the summary.
const summaryEvery = 3

const summaryInstruction = `【指示: 記録係】
上の【要約するターン】を、後のターンで思い出せるように、主語をはっきりさせた1文ずつにまとめてください。
- 3〜6文。1行に1文で、行頭に「- 」を付ける。
- 誰が・何を・どうなったかを書く（例:「- ガルドはゴブリンの斥候を倒した。」）。
- 判定の成否、手に入れた物、約束、わかった事実を優先する。
- 【これまでのあらすじ】に既にある内容は繰り返さない。
- 箇条書き以外は書かない。`

// MaybeSummarize folds finished turns older than the recent window into the
// session summary once summaryEvery of them have piled up. It returns whether
// a summary was written. Failing to summarize is reported but harmless: the
// turns stay unsummarized and are tried again after the next turn.
func (e *Engine) MaybeSummarize(ctx context.Context, sessionID int64, ev Events) (bool, error) {
	if ev == nil {
		ev = NoEvents{}
	}
	latest, err := db.LatestTurn(e.db, sessionID)
	if err != nil {
		return false, err
	}
	upto, err := db.SummarizedUpto(e.db, sessionID)
	if err != nil {
		return false, err
	}
	end := latest - recentTurns
	if end-upto < summaryEvery {
		return false, nil
	}

	results, err := db.ListTurnResults(e.db, sessionID)
	if err != nil {
		return false, err
	}
	var turns strings.Builder
	for _, raw := range results {
		var tr TurnResult
		if json.Unmarshal([]byte(raw), &tr) != nil || tr.Turn <= upto || tr.Turn > end {
			continue
		}
		fmt.Fprintf(&turns, "%s\n%s\n\n", playerLine(tr), turnRecap(tr))
	}
	if turns.Len() == 0 {
		return false, nil
	}

	sheets, err := db.GetNPCSheets(e.db, sessionID)
	if err != nil {
		return false, err
	}
	sc, err := e.loadContext(sessionID, sheets)
	if err != nil {
		return false, err
	}
	instruction := "【要約するターン】\n" + turns.String() + summaryInstruction

	var sentences []string
	_, _, err = e.runStage(ctx, stageRun{sessionID: sessionID, turn: latest, ev: ev}, stage{
		name: "summary", label: "あらすじ更新",
		opts:      llm.CompletionOptions{Temperature: 0.3, MaxTokens: 1024},
		retryHint: "「- 」で始まる1文ずつの箇条書きだけを、3〜6行で出力してください。",
		validate: func(res llm.Result) error {
			sentences = parseSummary(res.Text)
			if len(sentences) == 0 {
				return fmt.Errorf("「- 」で始まる行がありません")
			}
			return nil
		},
	}, []llm.Message{{Role: "system", Content: sc.system}, {Role: "user", Content: instruction}})
	if err != nil {
		return false, err
	}
	return true, db.AddSummary(e.db, sessionID, end, sentences)
}

// parseSummary keeps the bullet lines of a summary (at most 6), dropping
// anything else the model wrote around them.
func parseSummary(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, p := range []string{"- ", "・", "* ", "-"} {
			if rest, ok := strings.CutPrefix(line, p); ok {
				if rest = strings.TrimSpace(rest); rest != "" {
					out = append(out, rest)
				}
				break
			}
		}
		if len(out) == 6 {
			break
		}
	}
	return out
}
