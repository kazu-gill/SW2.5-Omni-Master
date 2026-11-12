package rag

import (
	"strings"
	"testing"

	"github.com/kf/sw25-omni-master/pkg/db"
)

func TestReadingIgnoresKanjiKanaAndConjugation(t *testing.T) {
	want := reading("かばう")
	for _, s := range []string{"庇う", "庇います", "仲間を庇った"} {
		if got := reading(s); !strings.Contains(got, want) {
			t.Errorf("reading(%q) = %q, want it to contain %q", s, got, want)
		}
	}
	if reading("剣の欠片") != reading("剣のかけら") {
		t.Errorf("欠片 / かけら: %q vs %q", reading("剣の欠片"), reading("剣のかけら"))
	}
}

func TestNormalizeTerm(t *testing.T) {
	for in, want := range map[string]string{
		"かばう（かばう）Ⅰ": "かばう", "全力攻撃Ⅱ": "全力攻撃", "武器習熟A／**": "武器習熟A", "エネルギー・ボルト": "エネルギーボルト",
	} {
		if got := normalizeTerm(in); got != want {
			t.Errorf("normalizeTerm(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTermIndexFindsDefiningChunk(t *testing.T) {
	idx := buildTermIndex([]db.RAGChunk{
		{ID: 1, Enabled: true, Section: "第三部 データ > 戦闘特技データ > 選択習得の宣言特技 > 《かばうⅠ》"},
		{ID: 2, Enabled: true, Section: "第三部 データ > 魔法データ > 真語魔法データ > 【エネルギー・ボルト】"},
		{ID: 3, Enabled: true, Section: "第二部 ルール > 戦闘のルール > ダメージ"},
		{ID: 4, Enabled: false, Section: "x > 《かばうⅡ》"},
	})
	got := idx.match("盾で割り込んで後衛を庇う", 2)
	if len(got) != 1 || got[0].chunkID != 1 || got[0].name != "かばう" {
		t.Errorf("庇う: %+v", got)
	}
	got = idx.match("エネルギーボルトを撃つ", 2)
	if len(got) != 1 || got[0].chunkID != 2 {
		t.Errorf("エネルギーボルト: %+v", got)
	}
	if got := idx.match("ゴブリンに斬りかかる", 2); len(got) != 0 {
		t.Errorf("no term expected: %+v", got)
	}
}
