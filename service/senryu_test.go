package service

import (
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/crypto"
)

const testEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func setupSenryuTestDB(t *testing.T) {
	t.Helper()
	var err error
	db.DB, err = gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	db.DB.AutoMigrate(&model.Senryu{}, &model.MutedChannel{}, &model.GuildChannelTypeSetting{})
	t.Cleanup(func() {
		db.DB.Close()
	})
}

func boolPtr(b bool) *bool {
	return &b
}

func TestCreateSenryu_暗号化有効時にDBに平文が保存されない(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	created, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "古池や",
		Nakasichi: "蛙飛び込む",
		Simogo:    "水の音",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	// Read raw data from DB (no decryption)
	var raw model.Senryu
	if err := db.DB.Where("id = ?", created.ID).First(&raw).Error; err != nil {
		t.Fatalf("failed to read raw senryu: %v", err)
	}

	if raw.Kamigo == "古池や" {
		t.Error("Kamigo should be encrypted in DB, but found plaintext")
	}
	if raw.Nakasichi == "蛙飛び込む" {
		t.Error("Nakasichi should be encrypted in DB, but found plaintext")
	}
	if raw.Simogo == "水の音" {
		t.Error("Simogo should be encrypted in DB, but found plaintext")
	}
}

func TestCreateSenryu_戻り値は平文フィールドを保持する(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	created, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "古池や",
		Nakasichi: "蛙飛び込む",
		Simogo:    "水の音",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	if created.Kamigo != "古池や" {
		t.Errorf("returned Kamigo should be plaintext, got %q", created.Kamigo)
	}
	if created.Nakasichi != "蛙飛び込む" {
		t.Errorf("returned Nakasichi should be plaintext, got %q", created.Nakasichi)
	}
	if created.Simogo != "水の音" {
		t.Errorf("returned Simogo should be plaintext, got %q", created.Simogo)
	}
	if created.ID == 0 {
		t.Error("returned ID should be non-zero (DB-assigned)")
	}
}

func TestGetSenryuByID_暗号化有効時に平文が復元される(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	created, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "柿くへば",
		Nakasichi: "鐘が鳴るなり",
		Simogo:    "法隆寺",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	got, err := GetSenryuByID(created.ID, "server1")
	if err != nil {
		t.Fatalf("GetSenryuByID failed: %v", err)
	}

	if got.Kamigo != "柿くへば" {
		t.Errorf("Kamigo: expected %q, got %q", "柿くへば", got.Kamigo)
	}
	if got.Nakasichi != "鐘が鳴るなり" {
		t.Errorf("Nakasichi: expected %q, got %q", "鐘が鳴るなり", got.Nakasichi)
	}
	if got.Simogo != "法隆寺" {
		t.Errorf("Simogo: expected %q, got %q", "法隆寺", got.Simogo)
	}
}

func TestGetLastSenryu_暗号化有効時に復号されたデータを返す(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	_, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "春すぎて",
		Nakasichi: "夏来にけらし",
		Simogo:    "白妙の",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	got, err := GetLastSenryu("server1")
	if err != nil {
		t.Fatalf("GetLastSenryu failed: %v", err)
	}

	if got.Kamigo != "春すぎて" {
		t.Errorf("Kamigo: expected %q, got %q", "春すぎて", got.Kamigo)
	}
	if got.Nakasichi != "夏来にけらし" {
		t.Errorf("Nakasichi: expected %q, got %q", "夏来にけらし", got.Nakasichi)
	}
	if got.Simogo != "白妙の" {
		t.Errorf("Simogo: expected %q, got %q", "白妙の", got.Simogo)
	}
}

func TestGetRecentSenryusByAuthor_暗号化有効時に復号されたリストを返す(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	_, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "閑さや",
		Nakasichi: "岩にしみ入る",
		Simogo:    "蝉の声",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	senryus, err := GetRecentSenryusByAuthor("server1", "author1", 10)
	if err != nil {
		t.Fatalf("GetRecentSenryusByAuthor failed: %v", err)
	}

	if len(senryus) != 1 {
		t.Fatalf("expected 1 senryu, got %d", len(senryus))
	}

	if senryus[0].Kamigo != "閑さや" {
		t.Errorf("Kamigo: expected %q, got %q", "閑さや", senryus[0].Kamigo)
	}
	if senryus[0].Nakasichi != "岩にしみ入る" {
		t.Errorf("Nakasichi: expected %q, got %q", "岩にしみ入る", senryus[0].Nakasichi)
	}
	if senryus[0].Simogo != "蝉の声" {
		t.Errorf("Simogo: expected %q, got %q", "蝉の声", senryus[0].Simogo)
	}
}

func TestGetThreeRandomSenryus_暗号化有効時に復号されたデータを返す(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	phrases := []struct{ k, n, s string }{
		{"五月雨を", "あつめて早し", "最上川"},
		{"荒海や", "佐渡によこたふ", "天の川"},
		{"夏草や", "兵どもが", "夢の跡"},
	}
	for _, p := range phrases {
		_, err := CreateSenryu(model.Senryu{
			ServerID:  "server1",
			AuthorID:  "author1",
			Kamigo:    p.k,
			Nakasichi: p.n,
			Simogo:    p.s,
			Spoiler:   &spoiler,
		})
		if err != nil {
			t.Fatalf("CreateSenryu failed: %v", err)
		}
	}

	senryus, err := GetThreeRandomSenryus("server1")
	if err != nil {
		t.Fatalf("GetThreeRandomSenryus failed: %v", err)
	}

	if len(senryus) != 3 {
		t.Fatalf("expected 3 senryus, got %d", len(senryus))
	}

	for i, s := range senryus {
		if s.Kamigo == "" || s.Nakasichi == "" || s.Simogo == "" {
			t.Errorf("senryu[%d] has empty fields after decryption", i)
		}
		if crypto.IsEncrypted(s.Kamigo) {
			t.Errorf("senryu[%d].Kamigo should be decrypted plaintext", i)
		}
	}
}

func TestGetTwoRandomNakasichi_件数不足と取得(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(""); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	got, err := GetTwoRandomNakasichi("server1")
	if err != nil {
		t.Fatalf("GetTwoRandomNakasichi failed: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil when empty, got %v", got)
	}

	spoiler := false
	_, err = CreateSenryu(model.Senryu{
		ServerID: "server1", AuthorID: "a1",
		Kamigo: "古池や", Nakasichi: "蛙飛び込む", Simogo: "水の音",
		Spoiler: &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	// count=1 still returns 2 rows (same row can be picked twice via offset sampling)
	got, err = GetTwoRandomNakasichi("server1")
	if err != nil {
		t.Fatalf("GetTwoRandomNakasichi failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 senryus even with 1 row, got %d", len(got))
	}
	if got[0].Nakasichi != "蛙飛び込む" || got[1].Nakasichi != "蛙飛び込む" {
		t.Errorf("unexpected nakasichi: %q, %q", got[0].Nakasichi, got[1].Nakasichi)
	}

	_, err = CreateSenryu(model.Senryu{
		ServerID: "server1", AuthorID: "a2",
		Kamigo: "五月雨を", Nakasichi: "あつめて早し", Simogo: "最上川",
		Spoiler: &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	got, err = GetTwoRandomNakasichi("server1")
	if err != nil {
		t.Fatalf("GetTwoRandomNakasichi failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 senryus, got %d", len(got))
	}
	for i, s := range got {
		if s.Nakasichi == "" {
			t.Errorf("senryu[%d].Nakasichi is empty", i)
		}
	}
}

func TestGetTwoRandomNakasichi_暗号化有効時に復号される(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	for _, p := range []struct{ k, n, s string }{
		{"五月雨を", "あつめて早し", "最上川"},
		{"荒海や", "佐渡によこたふ", "天の川"},
	} {
		_, err := CreateSenryu(model.Senryu{
			ServerID: "server1", AuthorID: "author1",
			Kamigo: p.k, Nakasichi: p.n, Simogo: p.s,
			Spoiler: &spoiler,
		})
		if err != nil {
			t.Fatalf("CreateSenryu failed: %v", err)
		}
	}

	got, err := GetTwoRandomNakasichi("server1")
	if err != nil {
		t.Fatalf("GetTwoRandomNakasichi failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	for i, s := range got {
		if crypto.IsEncrypted(s.Nakasichi) {
			t.Errorf("senryu[%d].Nakasichi should be decrypted", i)
		}
		if s.Nakasichi == "" {
			t.Errorf("senryu[%d].Nakasichi is empty", i)
		}
	}
}

func TestCreateSenryu_暗号化無効時に平文のまま保存される(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(""); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	created, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "古池や",
		Nakasichi: "蛙飛び込む",
		Simogo:    "水の音",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	var raw model.Senryu
	if err := db.DB.Where("id = ?", created.ID).First(&raw).Error; err != nil {
		t.Fatalf("failed to read raw senryu: %v", err)
	}

	if raw.Kamigo != "古池や" {
		t.Errorf("Kamigo should be plaintext when encryption is disabled, got %q", raw.Kamigo)
	}
	if raw.Nakasichi != "蛙飛び込む" {
		t.Errorf("Nakasichi should be plaintext when encryption is disabled, got %q", raw.Nakasichi)
	}
	if raw.Simogo != "水の音" {
		t.Errorf("Simogo should be plaintext when encryption is disabled, got %q", raw.Simogo)
	}
}

func TestMigration_平文データが暗号化される(t *testing.T) {
	setupSenryuTestDB(t)

	// Insert plaintext data with encryption disabled
	if err := crypto.Init(""); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}
	spoiler := false
	_, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "月見れば",
		Nakasichi: "千々にものこそ",
		Simogo:    "悲しけれ",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	// Enable encryption and run migration
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	// Simulate the migration (same logic as migrateEncryptSenryuData in db.go)
	var senryus []model.Senryu
	db.DB.Find(&senryus)
	for i := range senryus {
		s := &senryus[i]
		if crypto.IsEncrypted(s.Kamigo) {
			continue
		}
		kamigo, _ := crypto.Encrypt(s.Kamigo)
		nakasichi, _ := crypto.Encrypt(s.Nakasichi)
		simogo, _ := crypto.Encrypt(s.Simogo)
		db.DB.Model(s).Updates(map[string]interface{}{
			"kamigo":    kamigo,
			"nakasichi": nakasichi,
			"simogo":    simogo,
		})
	}

	// Verify raw DB data is encrypted
	var raw model.Senryu
	db.DB.First(&raw)
	if raw.Kamigo == "月見れば" {
		t.Error("Kamigo should be encrypted after migration")
	}

	// Verify decryption works correctly via service
	got, err := GetSenryuByID(raw.ID, "server1")
	if err != nil {
		t.Fatalf("GetSenryuByID failed: %v", err)
	}
	if got.Kamigo != "月見れば" {
		t.Errorf("expected %q after decryption, got %q", "月見れば", got.Kamigo)
	}
	if got.Nakasichi != "千々にものこそ" {
		t.Errorf("expected %q after decryption, got %q", "千々にものこそ", got.Nakasichi)
	}
	if got.Simogo != "悲しけれ" {
		t.Errorf("expected %q after decryption, got %q", "悲しけれ", got.Simogo)
	}
}

func TestMigration_暗号化済みデータは再暗号化されない(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	created, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "花の色は",
		Nakasichi: "うつりにけりな",
		Simogo:    "いたづらに",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	// Read raw encrypted data
	var before model.Senryu
	db.DB.Where("id = ?", created.ID).First(&before)

	// Run migration again (simulating restart)
	var senryus []model.Senryu
	db.DB.Find(&senryus)
	for i := range senryus {
		s := &senryus[i]
		if crypto.IsEncrypted(s.Kamigo) {
			continue // should skip
		}
		t.Error("should have detected already-encrypted data and skipped")
	}

	// Verify data is unchanged
	var after model.Senryu
	db.DB.Where("id = ?", created.ID).First(&after)
	if before.Kamigo != after.Kamigo {
		t.Error("encrypted data should not be modified by re-migration")
	}
}

func TestDeleteSenryu_暗号化有効時でも削除できる(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	created, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "削除テスト",
		Nakasichi: "暗号化されても",
		Simogo:    "消せるはず",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	if err := DeleteSenryu(created.ID, "server1"); err != nil {
		t.Fatalf("DeleteSenryu failed: %v", err)
	}

	_, err = GetSenryuByID(created.ID, "server1")
	if err != ErrSenryuNotFound {
		t.Errorf("expected ErrSenryuNotFound, got %v", err)
	}
}

func TestDeleteSenryusMatching_平文と暗号化の該当句だけ消える(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(""); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	keep, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "残す句の",
		Nakasichi: "上中下です",
		Simogo:    "消えない",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu keep failed: %v", err)
	}
	if _, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "テストです",
		Nakasichi: "この川柳に",
		Simogo:    "反応は",
		Spoiler:   &spoiler,
	}); err != nil {
		t.Fatalf("CreateSenryu excluded failed: %v", err)
	}

	n, err := DeleteSenryusMatching("テストです", "この川柳に", "反応は")
	if err != nil {
		t.Fatalf("DeleteSenryusMatching failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 deleted, got %d", n)
	}
	if _, err := GetSenryuByID(keep.ID, "server1"); err != nil {
		t.Fatalf("keep senryu should remain: %v", err)
	}

	// encrypted path
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}
	if _, err := CreateSenryu(model.Senryu{
		ServerID:  "server2",
		AuthorID:  "author1",
		Kamigo:    "テストです",
		Nakasichi: "この川柳に",
		Simogo:    "反応は",
		Spoiler:   &spoiler,
	}); err != nil {
		t.Fatalf("CreateSenryu encrypted excluded failed: %v", err)
	}
	keep2, err := CreateSenryu(model.Senryu{
		ServerID:  "server2",
		AuthorID:  "author1",
		Kamigo:    "暗号化でも",
		Nakasichi: "残すべき句",
		Simogo:    "ですよね",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu encrypted keep failed: %v", err)
	}

	n, err = DeleteSenryusMatching("テストです", "この川柳に", "反応は")
	if err != nil {
		t.Fatalf("DeleteSenryusMatching encrypted failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 encrypted deleted, got %d", n)
	}
	if _, err := GetSenryuByID(keep2.ID, "server2"); err != nil {
		t.Fatalf("encrypted keep senryu should remain: %v", err)
	}
}

func TestGetRanking_暗号化有効時でも集計できる(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	for i := 0; i < 3; i++ {
		_, err := CreateSenryu(model.Senryu{
			ServerID:  "server1",
			AuthorID:  "author1",
			Kamigo:    "ランキング",
			Nakasichi: "テストの句です",
			Simogo:    "数えよう",
			Spoiler:   &spoiler,
		})
		if err != nil {
			t.Fatalf("CreateSenryu failed: %v", err)
		}
	}

	ranks, err := GetRanking("server1")
	if err != nil {
		t.Fatalf("GetRanking failed: %v", err)
	}

	if len(ranks) != 1 {
		t.Fatalf("expected 1 rank entry, got %d", len(ranks))
	}
	if ranks[0].Count != 3 {
		t.Errorf("expected count 3, got %d", ranks[0].Count)
	}
	if ranks[0].AuthorId != "author1" {
		t.Errorf("expected author1, got %s", ranks[0].AuthorId)
	}
}

func seedSenryus(t *testing.T, serverID, authorID string, count int) {
	t.Helper()
	f := false
	for i := 0; i < count; i++ {
		s := model.Senryu{
			ServerID:  serverID,
			AuthorID:  authorID,
			Kamigo:    "上の句",
			Nakasichi: "中の句",
			Simogo:    "下の句",
			Spoiler:   &f,
		}
		if err := db.DB.Create(&s).Error; err != nil {
			t.Fatalf("failed to seed senryu: %v", err)
		}
	}
}

func TestGetSenryusByAuthorPaged_ページネーション(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")
	seedSenryus(t, "guild1", "user1", 30)

	// 1ページ目（25件）
	page1, err := GetSenryusByAuthorPaged("guild1", "user1", 25, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page1) != 25 {
		t.Errorf("expected 25 senryus, got %d", len(page1))
	}

	// 2ページ目（残り5件）
	page2, err := GetSenryusByAuthorPaged("guild1", "user1", 25, 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page2) != 5 {
		t.Errorf("expected 5 senryus, got %d", len(page2))
	}

	// IDの重複がないこと
	ids := make(map[int]bool)
	for _, s := range page1 {
		ids[s.ID] = true
	}
	for _, s := range page2 {
		if ids[s.ID] {
			t.Errorf("duplicate senryu ID %d across pages", s.ID)
		}
	}
}

func TestGetSenryusByAuthorPaged_降順(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")
	seedSenryus(t, "guild1", "user1", 5)

	results, err := GetSenryusByAuthorPaged("guild1", "user1", 25, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i := 1; i < len(results); i++ {
		if results[i].ID >= results[i-1].ID {
			t.Errorf("expected descending order: ID %d >= %d", results[i].ID, results[i-1].ID)
		}
	}
}

func TestGetSenryusByAuthorPaged_該当なし(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")

	results, err := GetSenryusByAuthorPaged("guild1", "user1", 25, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 senryus, got %d", len(results))
	}
}

func TestGetSenryusByAuthorPaged_別サーバーの川柳は含まない(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")
	seedSenryus(t, "guild1", "user1", 5)
	seedSenryus(t, "guild2", "user1", 3)

	results, err := GetSenryusByAuthorPaged("guild1", "user1", 25, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("expected 5 senryus for guild1, got %d", len(results))
	}
}

func TestGetSenryusByAuthorPaged_別ユーザーの川柳は含まない(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")
	seedSenryus(t, "guild1", "user1", 5)
	seedSenryus(t, "guild1", "user2", 3)

	results, err := GetSenryusByAuthorPaged("guild1", "user1", 25, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("expected 5 senryus for user1, got %d", len(results))
	}
}

func TestGetSenryusByAuthorPaged_offset超過で空(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")
	seedSenryus(t, "guild1", "user1", 3)

	results, err := GetSenryusByAuthorPaged("guild1", "user1", 25, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 senryus with large offset, got %d", len(results))
	}
}

func TestCountSenryusByAuthor_正常(t *testing.T) {
	setupSenryuTestDB(t)
	seedSenryus(t, "guild1", "user1", 30)

	count, err := CountSenryusByAuthor("guild1", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 30 {
		t.Errorf("expected 30, got %d", count)
	}
}

func TestCountSenryusByAuthor_該当なし(t *testing.T) {
	setupSenryuTestDB(t)

	count, err := CountSenryusByAuthor("guild1", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestCountSenryusByAuthor_別サーバーは含まない(t *testing.T) {
	setupSenryuTestDB(t)
	seedSenryus(t, "guild1", "user1", 5)
	seedSenryus(t, "guild2", "user1", 3)

	count, err := CountSenryusByAuthor("guild1", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5, got %d", count)
	}
}

func TestCountSenryusByAuthor_別ユーザーは含まない(t *testing.T) {
	setupSenryuTestDB(t)
	seedSenryus(t, "guild1", "user1", 5)
	seedSenryus(t, "guild1", "user2", 3)

	count, err := CountSenryusByAuthor("guild1", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5, got %d", count)
	}
}

func TestGetServerStats_正常系(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")
	seedSenryus(t, "guild1", "user1", 3)
	seedSenryus(t, "guild1", "user2", 2)

	stats, err := GetServerStats("guild1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.TotalSenryus != 5 {
		t.Errorf("TotalSenryus = %d, want 5", stats.TotalSenryus)
	}
	if stats.UniqueAuthors != 2 {
		t.Errorf("UniqueAuthors = %d, want 2", stats.UniqueAuthors)
	}
}

func TestGetServerStats_川柳が0件(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")

	stats, err := GetServerStats("guild1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.TotalSenryus != 0 {
		t.Errorf("TotalSenryus = %d, want 0", stats.TotalSenryus)
	}
	if stats.UniqueAuthors != 0 {
		t.Errorf("UniqueAuthors = %d, want 0", stats.UniqueAuthors)
	}
}

func TestGetServerStats_別サーバーの川柳は含まない(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")
	seedSenryus(t, "guild1", "user1", 3)
	seedSenryus(t, "guild2", "user2", 5)

	stats, err := GetServerStats("guild1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.TotalSenryus != 3 {
		t.Errorf("TotalSenryus = %d, want 3", stats.TotalSenryus)
	}
	if stats.UniqueAuthors != 1 {
		t.Errorf("UniqueAuthors = %d, want 1", stats.UniqueAuthors)
	}
}

func TestGetServerStats_同一ユーザーが複数句(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")
	seedSenryus(t, "guild1", "user1", 7)

	stats, err := GetServerStats("guild1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.TotalSenryus != 7 {
		t.Errorf("TotalSenryus = %d, want 7", stats.TotalSenryus)
	}
	if stats.UniqueAuthors != 1 {
		t.Errorf("UniqueAuthors = %d, want 1", stats.UniqueAuthors)
	}
}

func TestGetLastSenryu_最後の川柳を返す(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(""); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	db.DB.Create(&model.Senryu{
		ServerID: "guild1", AuthorID: "user1",
		Kamigo: "古池や", Nakasichi: "蛙飛び込む", Simogo: "水の音",
		Spoiler: boolPtr(false),
	})
	db.DB.Create(&model.Senryu{
		ServerID: "guild1", AuthorID: "user2",
		Kamigo: "柿くへば", Nakasichi: "鐘が鳴るなり", Simogo: "法隆寺",
		Spoiler: boolPtr(false),
	})

	got, err := GetLastSenryu("guild1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AuthorID != "user2" {
		t.Errorf("AuthorID = %q, want %q", got.AuthorID, "user2")
	}
	if got.Kamigo != "柿くへば" {
		t.Errorf("Kamigo = %q, want %q", got.Kamigo, "柿くへば")
	}
	if got.Nakasichi != "鐘が鳴るなり" {
		t.Errorf("Nakasichi = %q, want %q", got.Nakasichi, "鐘が鳴るなり")
	}
	if got.Simogo != "法隆寺" {
		t.Errorf("Simogo = %q, want %q", got.Simogo, "法隆寺")
	}
}

func TestGetLastSenryu_川柳が存在しない場合(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(""); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	_, err := GetLastSenryu("guild1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrSenryuNotFound) {
		t.Errorf("error = %v, want ErrSenryuNotFound", err)
	}
}

func TestGetLastSenryu_サーバーごとに独立(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(""); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	db.DB.Create(&model.Senryu{
		ServerID: "guild1", AuthorID: "user1",
		Kamigo: "古池や", Nakasichi: "蛙飛び込む", Simogo: "水の音",
		Spoiler: boolPtr(false),
	})
	db.DB.Create(&model.Senryu{
		ServerID: "guild2", AuthorID: "user2",
		Kamigo: "柿くへば", Nakasichi: "鐘が鳴るなり", Simogo: "法隆寺",
		Spoiler: boolPtr(false),
	})

	got, err := GetLastSenryu("guild1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AuthorID != "user1" {
		t.Errorf("AuthorID = %q, want %q", got.AuthorID, "user1")
	}
	if got.ServerID != "guild1" {
		t.Errorf("ServerID = %q, want %q", got.ServerID, "guild1")
	}
}

func TestGetLastSenryu_スポイラー付き川柳(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(""); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	db.DB.Create(&model.Senryu{
		ServerID: "guild1", AuthorID: "user1",
		Kamigo: "秘密の", Nakasichi: "内容が含まれる", Simogo: "川柳だ",
		Spoiler: boolPtr(true),
	})

	got, err := GetLastSenryu("guild1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Spoiler == nil || !*got.Spoiler {
		t.Error("Spoiler should be true")
	}
}

func TestGetLastSenryu_スポイラーなし川柳(t *testing.T) {
	setupSenryuTestDB(t)
	if err := crypto.Init(""); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	db.DB.Create(&model.Senryu{
		ServerID: "guild1", AuthorID: "user1",
		Kamigo: "古池や", Nakasichi: "蛙飛び込む", Simogo: "水の音",
		Spoiler: boolPtr(false),
	})

	got, err := GetLastSenryu("guild1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Spoiler == nil || *got.Spoiler {
		t.Error("Spoiler should be false")
	}
}

func seedSenryuAt(t *testing.T, serverID, authorID string, createdAt time.Time) model.Senryu {
	t.Helper()
	f := false
	s := model.Senryu{
		ServerID:  serverID,
		AuthorID:  authorID,
		Kamigo:    "古池や",
		Nakasichi: "蛙飛び込む",
		Simogo:    "水の音",
		Spoiler:   &f,
		CreatedAt: createdAt,
	}
	if err := db.DB.Create(&s).Error; err != nil {
		t.Fatalf("failed to seed senryu: %v", err)
	}
	return s
}

func TestCountSenryusFiltered_ユーザーと期間(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")

	day1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	day3 := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)

	seedSenryuAt(t, "guild1", "user1", day1)
	seedSenryuAt(t, "guild1", "user1", day2)
	seedSenryuAt(t, "guild1", "user1", day3)
	seedSenryuAt(t, "guild1", "user2", day2)
	seedSenryuAt(t, "guild2", "user1", day2)

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	count, err := CountSenryusFiltered(SenryuFilter{
		ServerID: "guild1",
		AuthorID: "user1",
		From:     from,
		To:       to,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestCountSenryusFiltered_期間のみ全ユーザー(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")

	day := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	seedSenryuAt(t, "guild1", "user1", day)
	seedSenryuAt(t, "guild1", "user2", day)
	seedSenryuAt(t, "guild1", "user1", time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC))

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	count, err := CountSenryusFiltered(SenryuFilter{
		ServerID: "guild1",
		From:     from,
		To:       to,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestDeleteSenryusFiltered_該当のみ削除(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")

	day1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)
	seedSenryuAt(t, "guild1", "user1", day1)
	seedSenryuAt(t, "guild1", "user1", day2)
	seedSenryuAt(t, "guild1", "user2", day1)

	deleted, err := DeleteSenryusFiltered(SenryuFilter{
		ServerID: "guild1",
		AuthorID: "user1",
		From:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	remaining, err := CountSenryusByAuthor("guild1", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if remaining != 1 {
		t.Errorf("expected 1 remaining for user1, got %d", remaining)
	}

	user2Count, err := CountSenryusByAuthor("guild1", "user2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user2Count != 1 {
		t.Errorf("expected user2 untouched, got %d", user2Count)
	}
}

func TestDeleteSenryusFiltered_ユーザーのみ(t *testing.T) {
	setupSenryuTestDB(t)
	crypto.Init("")
	seedSenryus(t, "guild1", "user1", 3)
	seedSenryus(t, "guild1", "user2", 2)

	deleted, err := DeleteSenryusFiltered(SenryuFilter{
		ServerID: "guild1",
		AuthorID: "user1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 3 {
		t.Errorf("expected 3 deleted, got %d", deleted)
	}

	remaining, err := CountSenryusByAuthor("guild1", "user2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if remaining != 2 {
		t.Errorf("expected 2 for user2, got %d", remaining)
	}
}
