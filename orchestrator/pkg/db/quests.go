package db

import (
	"database/sql"
	"fmt"
)

type Quest struct {
	ID          int64  `json:"id"`
	Rank        string `json:"rank"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Client      string `json:"client"`
	Reward      string `json:"reward"`
	Target      string `json:"target"`
	Level       string `json:"level"`
	Tags        string `json:"tags"`
	Status      string `json:"status"` // available | active | completed
}

var seedQuests = []Quest{
	{Rank: "B", Title: "廃坑の蜘蛛女王", Description: "近郊の廃坑にアラクネが巣食い、採掘者が行方不明になっている。討伐と生存者の救出を依頼する。", Client: "エロルド辺境伯", Reward: "1,800G", Target: "アラクネ討伐・生存者救出", Level: "Lv.3〜5", Tags: "combat,dungeon"},
	{Rank: "A", Title: "竜の目覚め", Description: "北の山脈に眠っていたワイバーンが目覚め、村を脅かし始めた。討伐か対話か、選択が未来を変える。", Client: "山間の村長連合", Reward: "4,500G", Target: "ワイバーン対処", Level: "Lv.5〜8", Tags: "combat,explore,social"},
	{Rank: "C", Title: "迷子の使い魔", Description: "魔法使いの老婆が使い魔のフェレットを探している。街中を駆け回る小さな冒険。", Client: "魔女エリシア", Reward: "300G", Target: "使い魔フェレット発見・保護", Level: "Lv.1〜2", Tags: "social"},
	{Rank: "S", Title: "古代遺跡の封印", Description: "数百年前に封印された遺跡が再び活性化し始めた。内部に潜む脅威を調査し、再封印せよ。", Client: "魔術師ギルド総長", Reward: "12,000G", Target: "遺跡調査・再封印完了", Level: "Lv.8〜12", Tags: "dungeon,combat"},
	{Rank: "B", Title: "商隊の護衛", Description: "王都から辺境の街への物資輸送。山賊の出没が報告されており、熟練の護衛が必要だ。", Client: "ダルハン商会", Reward: "2,200G", Target: "商隊を無事に目的地まで護衛", Level: "Lv.3〜6", Tags: "combat,social"},
	{Rank: "A", Title: "呪われた森の調査", Description: "かつて豊かだった森が突然枯れ始め、動物たちが狂暴化した。原因を突き止めよ。", Client: "森の精霊評議会", Reward: "3,800G + 精霊の加護", Target: "呪いの原因解明・解除", Level: "Lv.5〜7", Tags: "explore,dungeon,social"},
}

func ListQuests(d *sql.DB) ([]Quest, error) {
	rows, err := d.Query(
		`SELECT id, rank, title, description, client, reward, target, level, tags, status
		 FROM quests ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'available' THEN 1 ELSE 2 END, rank DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Quest
	for rows.Next() {
		var q Quest
		if err := rows.Scan(&q.ID, &q.Rank, &q.Title, &q.Description, &q.Client, &q.Reward, &q.Target, &q.Level, &q.Tags, &q.Status); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func AcceptQuest(d *sql.DB, id int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	// Set any currently active quest back to available
	if _, err := tx.Exec(`UPDATE quests SET status = 'available' WHERE status = 'active'`); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE quests SET status = 'active' WHERE id = ? AND status = 'available'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("quest %d not available", id)
	}
	return tx.Commit()
}

func CompleteQuest(d *sql.DB, id int64) error {
	_, err := d.Exec(`UPDATE quests SET status = 'completed' WHERE id = ?`, id)
	return err
}

// SeedQuests inserts default quests if the table is empty.
func SeedQuests(d *sql.DB) error {
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM quests`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, q := range seedQuests {
		_, err := d.Exec(
			`INSERT INTO quests (rank, title, description, client, reward, target, level, tags) VALUES (?,?,?,?,?,?,?,?)`,
			q.Rank, q.Title, q.Description, q.Client, q.Reward, q.Target, q.Level, q.Tags,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
