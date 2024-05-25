package zaya

import (
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"go.uber.org/zap/zaptest"
	"testing"
	"time"
)

func setupAiChat(t *testing.T) *aiChat {
	logger := zaptest.NewLogger(t).Sugar()
	chat := newAiChat("Hello", 100, 10, logger)

	require.NotNil(t, chat)
	require.Equal(t, 1, chat.getMessageCount())
	require.Equal(t, "Hello", chat.getMessageText(0))

	return chat
}

func (chat *aiChat) getMessageText(i int) string {
	if i >= len(chat.messages) {
		return ""
	}

	part := chat.messages[i].Parts[0]
	return part.(llms.TextContent).Text
}

func (chat *aiChat) getMessageCount() int {
	return len(chat.messages)
}

func testHistoryLimit(t *testing.T, chat *aiChat) {
	for i := 0; i < 12; i++ {
		if i%2 == 0 {
			chat.addUserMessage("12345")
		} else {
			chat.addBotMessage("12345", 50)
		}
	}

	require.Equal(t, 11, chat.getMessageCount())
}

func testContextLimit(t *testing.T, chat *aiChat) {
	for i := 0; i < 6; i++ {
		if i%2 == 0 {
			chat.addUserMessage("12345678901234567890")
		} else {
			chat.addBotMessage("12345678901234567890", 50)
		}
	}

	require.Equal(t, 5, chat.getMessageCount())
}

func TestNewAiChat(t *testing.T) {
	setupAiChat(t)
}

func TestAddMessage(t *testing.T) {
	chat := setupAiChat(t)

	chat.addUserMessage("Hi there!")
	require.Equal(t, 2, chat.getMessageCount())
	require.Equal(t, "Hi there!", chat.getMessageText(1))

	chat.addBotMessage("Hello, human!", 50)
	require.Equal(t, 3, chat.getMessageCount())
	require.Equal(t, "Hello, human!", chat.getMessageText(2))
}

func TestChatLimits(t *testing.T) {
	chat := setupAiChat(t)
	testHistoryLimit(t, chat)

	chat = setupAiChat(t)
	testContextLimit(t, chat)
}

func TestCleanHistory(t *testing.T) {
	chat := setupAiChat(t)
	testHistoryLimit(t, chat)

	chat.cleanHistory()
	require.Equal(t, 9, chat.getMessageCount())
	testContextLimit(t, chat)
}

func TestIsExpired(t *testing.T) {
	chat := setupAiChat(t)
	time.Sleep(5 * time.Millisecond)
	require.True(t, chat.isExpired(1*time.Millisecond))
	require.False(t, chat.isExpired(10*time.Millisecond))
}

func TestRestart(t *testing.T) {
	chat := setupAiChat(t)
	testHistoryLimit(t, chat)
	chat.restart()
	require.Equal(t, 1, chat.getMessageCount())
	testHistoryLimit(t, chat)
	chat.restart()
	require.Equal(t, 1, chat.getMessageCount())
	testContextLimit(t, chat)
}
