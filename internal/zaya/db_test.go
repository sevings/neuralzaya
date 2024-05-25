package zaya

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

var defaultCfg = ChatConfig{
	Freq:       10,
	MaxHistory: 20,
	Nickname:   "test",
	Prompt:     "prompt",
}

func setupTestDB(t *testing.T) *DB {
	tmpFile, err := os.CreateTemp("", "crocodiler")
	if err != nil {
		t.Fatalf("Can't create temp file for a test DB")
	}

	path := tmpFile.Name()
	_ = tmpFile.Close()
	t.Cleanup(func() { _ = os.Remove(path) })

	db, success := LoadDatabase(path, defaultCfg)
	require.True(t, success)
	require.NotNil(t, db)

	return db
}

func createMockTOMLFile(t *testing.T) string {
	content := `
[[Roles]]
Name = "role1"
Lang = "en"
Nickname = "nick1"
Prompt = "prompt1"
`
	file, err := os.CreateTemp("", "*.toml")
	require.NoError(t, err)

	_, err = file.Write([]byte(content))
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	path := file.Name()

	t.Cleanup(func() {
		_ = os.Remove(path)
	})

	return path
}

func TestLoadDatabase(t *testing.T) {
	setupTestDB(t)
}

func TestLoadChatConfig(t *testing.T) {
	db := setupTestDB(t)

	cfg := db.LoadChatConfig(1)
	require.Equal(t, int64(1), cfg.ChatID)
	require.Equal(t, defaultCfg.Freq, cfg.Freq)
	require.Equal(t, defaultCfg.MaxHistory, cfg.MaxHistory)
	require.Equal(t, defaultCfg.Nickname, cfg.Nickname)
	require.Equal(t, defaultCfg.Prompt, cfg.Prompt)

	cfg = db.LoadChatConfig(1)
	require.Equal(t, int64(1), cfg.ChatID)
}

func TestSetFreq(t *testing.T) {
	db := setupTestDB(t)

	db.SetFreq(1, 15)
	cfg := db.LoadChatConfig(1)
	require.Equal(t, 15, cfg.Freq)

	db.SetFreq(1, 20)
	cfg = db.LoadChatConfig(1)
	require.Equal(t, 20, cfg.Freq)
}

func TestSetNickname(t *testing.T) {
	db := setupTestDB(t)

	db.SetNickname(1, "new_nickname")
	cfg := db.LoadChatConfig(1)
	require.Equal(t, "new_nickname", cfg.Nickname)

	db.SetNickname(1, "another_nickname")
	cfg = db.LoadChatConfig(1)
	require.Equal(t, "another_nickname", cfg.Nickname)
}

func TestSetPrompt(t *testing.T) {
	db := setupTestDB(t)

	db.SetPrompt(1, "new_prompt")
	cfg := db.LoadChatConfig(1)
	require.Equal(t, "new_prompt", cfg.Prompt)

	db.SetPrompt(1, "another_prompt")
	cfg = db.LoadChatConfig(1)
	require.Equal(t, "another_prompt", cfg.Prompt)
}

func TestSetMaxHistory(t *testing.T) {
	db := setupTestDB(t)

	db.SetMaxHistory(1, 30)
	cfg := db.LoadChatConfig(1)
	require.Equal(t, 30, cfg.MaxHistory)

	db.SetMaxHistory(1, 10)
	cfg = db.LoadChatConfig(1)
	require.Equal(t, 10, cfg.MaxHistory)
}

func TestLoadChatRoleNames(t *testing.T) {
	db := setupTestDB(t)

	roles := db.LoadChatRoleNames(1)
	require.Empty(t, roles)
}

func TestUploadGlobalRoles(t *testing.T) {
	db := setupTestDB(t)

	roles := db.LoadAllRoleNames(1)
	require.Empty(t, roles)

	mockTOMLPath := createMockTOMLFile(t)
	db.UploadGlobalRoles(mockTOMLPath)

	roles = db.LoadAllRoleNames(1)
	require.Equal(t, 1, len(roles))
	require.Equal(t, roles[0].Name, "role1")

	roles = db.LoadChatRoleNames(1)
	require.Empty(t, roles)
}

func TestSaveRole(t *testing.T) {
	db := setupTestDB(t)

	db.SaveRole(1, "en", "role_name")

	roles := db.LoadAllRoleNames(1)
	require.Equal(t, 1, len(roles))
	require.Equal(t, roles[0].Name, "role_name")

	roles = db.LoadChatRoleNames(1)
	require.Equal(t, 1, len(roles))
	require.Equal(t, roles[0].Name, "role_name")

	roles = db.LoadAllRoleNames(2)
	require.Empty(t, roles)
	roles = db.LoadChatRoleNames(2)
	require.Empty(t, roles)
}

func TestRemoveRole(t *testing.T) {
	db := setupTestDB(t)

	mockTOMLPath := createMockTOMLFile(t)
	db.UploadGlobalRoles(mockTOMLPath)

	db.SaveRole(1, "en", "role_name")
	roles := db.LoadChatRoleNames(1)
	ok := db.RemoveRole(2, roles[0].ID)
	require.False(t, ok)
	ok = db.RemoveRole(1, roles[0].ID)
	require.True(t, ok)

	roles = db.LoadAllRoleNames(1)
	ok = db.RemoveRole(1, roles[0].ID)
	require.False(t, ok)
}

func TestSetRole(t *testing.T) {
	db := setupTestDB(t)

	db.SaveRole(1, "en", "role_name")
	roles := db.LoadChatRoleNames(1)
	setRole, success := db.SetRole(1, roles[0].ID)
	require.True(t, success)
	require.Equal(t, roles[0].Name, setRole.Name)

	_, success = db.SetRole(2, roles[0].ID)
	require.False(t, success)
}
